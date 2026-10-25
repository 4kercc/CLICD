#!/usr/bin/env node
/**
 * CLICD MCP server.
 *
 * Exposes one or more CLICD panels as MCP tools over stdio so an agent can
 * inspect templates/storage/instances and create or manage LXC containers and
 * KVM virtual machines.
 *
 * Environment:
 *   CLICD_BASE_URL   Overrides / defines the default panel URL
 *   CLICD_API_KEY    API key for that panel, "clicd_sk_..."
 *   CLICD_PANELS     JSON object of extra panels, e.g.
 *                    {"绿云":{"baseUrl":"http://1.2.3.4:8999","apiKey":"clicd_sk_..."}}
 *   CLICD_TIMEOUT_MS Default request timeout in ms (optional, 120000)
 *   CLICD_INSECURE   Set to "1" to accept self-signed HTTPS certificates
 *
 * Local config (not committed):
 *   clicd-mcp.local.json  {"defaultPanel":"独服","panels":{"独服":{...},"绿云":{...}}}
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
let localConfig = {};
try {
  localConfig = JSON.parse(readFileSync(join(here, "clicd-mcp.local.json"), "utf8"));
} catch {
  localConfig = {};
}

function normalizeUrl(value) {
  return String(value || "").replace(/\/+$/, "");
}

/** panel name -> { baseUrl, apiKey } */
const panels = new Map();
function registerPanel(name, cfg) {
  const baseUrl = normalizeUrl(cfg?.baseUrl);
  const apiKey = String(cfg?.apiKey || "").trim();
  if (!name || !baseUrl || !apiKey) return;
  panels.set(name, { baseUrl, apiKey });
}

if (localConfig.panels && typeof localConfig.panels === "object") {
  for (const [name, cfg] of Object.entries(localConfig.panels)) registerPanel(name, cfg);
}
// Legacy single-panel configuration.
if (localConfig.baseUrl) {
  registerPanel(localConfig.name || "默认", { baseUrl: localConfig.baseUrl, apiKey: localConfig.apiKey });
}
// Environment overrides always win for the default panel.
if (process.env.CLICD_BASE_URL) {
  registerPanel(localConfig.defaultPanel || localConfig.name || "默认", {
    baseUrl: process.env.CLICD_BASE_URL,
    apiKey: process.env.CLICD_API_KEY || localConfig.apiKey,
  });
}
if (process.env.CLICD_PANELS) {
  try {
    for (const [name, cfg] of Object.entries(JSON.parse(process.env.CLICD_PANELS))) registerPanel(name, cfg);
  } catch (err) {
    console.error(`clicd-mcp: CLICD_PANELS is not valid JSON: ${err.message}`);
  }
}

const DEFAULT_PANEL =
  localConfig.defaultPanel && panels.has(localConfig.defaultPanel)
    ? localConfig.defaultPanel
    : panels.keys().next().value;

const DEFAULT_TIMEOUT = Number(process.env.CLICD_TIMEOUT_MS || localConfig.timeoutMs || 120000);
const CREATE_TIMEOUT = Number(
  process.env.CLICD_CREATE_TIMEOUT_MS || localConfig.createTimeoutMs || 1800000
);
const TASK_TIMEOUT = Number(
  process.env.CLICD_TASK_TIMEOUT_MS || localConfig.taskTimeoutMs || 900000
);

if (process.env.CLICD_INSECURE === "1" || localConfig.insecure === true) {
  process.env.NODE_TLS_REJECT_UNAUTHORIZED = "0";
}

if (panels.size === 0) {
  console.error(
    "clicd-mcp: no panel configured. Set CLICD_BASE_URL + CLICD_API_KEY, or create clicd-mcp.local.json (see mcp/clicd-mcp/README.md)"
  );
  process.exit(1);
}

const PANEL_NAMES = [...panels.keys()];

function resolvePanel(name) {
  const key = name || DEFAULT_PANEL;
  const panel = panels.get(key);
  if (!panel) {
    throw new Error(`未知面板 "${key}"，可用面板：${PANEL_NAMES.join(" / ")}`);
  }
  return { name: key, ...panel };
}

/** Perform one CLICD API call and unwrap the {success,data,message} envelope. */
async function api(panelName, method, path, body, timeoutMs = DEFAULT_TIMEOUT) {
  const panel = resolvePanel(panelName);
  const url = `${panel.baseUrl}${path}`;
  const headers = {
    Authorization: `Bearer ${panel.apiKey}`,
    Accept: "application/json",
  };
  const init = { method, headers, signal: AbortSignal.timeout(timeoutMs) };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }

  let res;
  try {
    res = await fetch(url, init);
  } catch (err) {
    throw new Error(`[${panel.name}] request failed: ${method} ${path}: ${err.message}`);
  }

  const text = await res.text();
  let payload;
  try {
    payload = text ? JSON.parse(text) : {};
  } catch {
    payload = { success: res.ok, message: text };
  }

  if (!res.ok || payload.success === false) {
    const message = payload.message || payload.error || text || res.statusText;
    const code = payload.code ? ` (code ${payload.code})` : "";
    throw new Error(`[${panel.name}] API ${res.status}${code}: ${message}`);
  }
  return payload.data !== undefined ? payload.data : payload;
}

function ok(data) {
  return { content: [{ type: "text", text: JSON.stringify(data, null, 2) }] };
}

function fail(err) {
  const message = err instanceof Error ? err.message : String(err);
  return { isError: true, content: [{ type: "text", text: message }] };
}

const panelField = z
  .string()
  .optional()
  .describe(`面板名称（${PANEL_NAMES.join(" / ")}），默认 ${DEFAULT_PANEL}`);

function summarizeContainer(panelName, c) {
  const virtualization =
    c.virtualization || (c.kvm_name ? "kvm" : c.lxc_name ? "lxc" : "lxc");
  return {
    panel: panelName,
    id: c.id,
    name: c.name,
    uuid: c.uuid,
    virtualization,
    status: c.status,
    template: c.template,
    ip: c.ip,
    ssh_port: c.ssh_port,
    vnc_port: virtualization === "kvm" ? c.vnc_port : undefined,
    vcpu: c.vcpu,
    ram_mb: c.ram_mb,
    disk_gb: c.disk_gb,
    nat_ports: (c.port_mappings || []).map((m) => ({
      host_port: m.host_port,
      container_port: m.container_port,
      protocol: m.protocol,
    })),
    public_ipv4s: (c.public_ipv4s || []).map((p) => p.address).filter(Boolean),
    ipv6: c.ipv6 || undefined,
    expires_at: c.expires_at || undefined,
    created_at: c.created_at,
  };
}

async function findContainer(panelName, identifier) {
  const list = await api(panelName, "GET", "/api/v1/containers");
  const needle = String(identifier).toLowerCase();
  return (
    list.find(
      (c) =>
        String(c.id) === String(identifier) ||
        c.uuid === identifier ||
        String(c.name).toLowerCase() === needle
    ) || null
  );
}

async function resolveVirtualization(panelName, templateId) {
  for (const runtime of ["lxc", "kvm"]) {
    const images = await api(
      panelName,
      "GET",
      `/api/v1/images/enabled?type=${encodeURIComponent(runtime)}`
    );
    if (images.some((img) => img.id === templateId)) return runtime;
  }
  return "lxc";
}

function buildCreatePayload(a) {
  const virt = a.virtualization || "lxc";
  const payload = {
    name: a.name,
    template_id: a.template_id,
    virtualization: virt,
    vcpu: a.vcpu ?? (virt === "kvm" ? 2 : 1),
    ram_mb: a.ram_mb ?? (virt === "kvm" ? 2048 : 512),
    disk_gb: a.disk_gb ?? (virt === "kvm" ? 30 : 5),
    assign_nat: a.assign_nat ?? true,
    port_mapping_count: a.port_mapping_count ?? 0,
    snapshot_limit: a.snapshot_limit ?? 3,
  };
  const optional = [
    "management_port",
    "storage_pool_id",
    "ssh_auth_mode",
    "ssh_password",
    "ssh_public_key",
    "init_script",
    "expires_at",
    "boot_media",
    "extra_iso",
    "network_down_mbps",
    "network_up_mbps",
    "monthly_traffic_gb",
  ];
  for (const key of optional) {
    if (a[key] !== undefined && a[key] !== "") payload[key] = a[key];
  }
  if (a.assign_ipv4) {
    payload.assign_ipv4 = true;
    payload.ipv4_count = a.ipv4_count ?? 1;
  }
  if (a.assign_ipv6) {
    payload.assign_ipv6 = true;
    payload.ipv6_count = a.ipv6_count ?? 1;
  }
  return payload;
}

const containerSpecSchema = {
  name: z.string().min(1).describe("Instance name (unique, letters/digits/hyphen)"),
  template_id: z.string().min(1).describe("Image/template ID from clicd_list_templates"),
  virtualization: z.enum(["lxc", "kvm"]).optional().describe("Runtime; auto-detected from the template when omitted"),
  vcpu: z.number().positive().optional().describe("vCPU count (default 1 LXC / 2 KVM)"),
  ram_mb: z.number().int().positive().optional().describe("RAM in MB (default 512 LXC / 2048 KVM)"),
  disk_gb: z.number().int().positive().optional().describe("Disk size in GB (default 5 LXC / 30 KVM)"),
  assign_nat: z.boolean().optional().describe("Attach internal NAT networking (default true)"),
  port_mapping_count: z.number().int().min(0).max(64).optional().describe("Extra NAT port mappings to auto-allocate"),
  management_port: z.number().int().min(1).max(65535).optional().describe("Specific NAT host port for the management/SSH port"),
  assign_ipv4: z.boolean().optional().describe("Allocate a public IPv4 address"),
  ipv4_count: z.number().int().min(1).max(64).optional().describe("Number of public IPv4s (default 1)"),
  assign_ipv6: z.boolean().optional().describe("Allocate public IPv6 addresses"),
  ipv6_count: z.number().int().min(1).max(64).optional().describe("Number of IPv6 addresses (default 1)"),
  ssh_auth_mode: z.enum(["auto_password", "password", "key"]).optional().describe("SSH auth mode; auto_password when omitted"),
  ssh_password: z.string().optional().describe("Custom SSH password (with ssh_auth_mode=password)"),
  ssh_public_key: z.string().optional().describe("SSH public key (with ssh_auth_mode=key)"),
  init_script: z.string().optional().describe("Post-install shell commands (newline separated)"),
  storage_pool_id: z.string().optional().describe("Storage pool ID; default pool when omitted"),
  snapshot_limit: z.number().int().min(1).optional().describe("Snapshot quota (default 3)"),
  expires_at: z.string().optional().describe("Expiry timestamp, e.g. 2026-12-31 23:59:59"),
  network_down_mbps: z.number().int().min(0).optional().describe("Download limit in Mbps (0 = unlimited)"),
  network_up_mbps: z.number().int().min(0).optional().describe("Upload limit in Mbps (0 = unlimited)"),
  monthly_traffic_gb: z.number().int().min(0).optional().describe("Traffic cap in GB (0 = unlimited)"),
  boot_media: z.string().optional().describe("KVM only: override boot ISO path/ID"),
  extra_iso: z.string().optional().describe("KVM only: additional CD-ROM ISO path/ID"),
};

async function waitForTask(panelName, taskId, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let last = null;
  while (Date.now() < deadline) {
    const tasks = await api(panelName, "GET", "/api/v1/tasks");
    last = (tasks || []).find((t) => String(t.id) === String(taskId)) || last;
    if (last && (last.status === "done" || last.status === "failed")) {
      return last;
    }
    await new Promise((resolve) => setTimeout(resolve, 2000));
  }
  return last || { id: taskId, status: "unknown", error: "timed out waiting for task" };
}

async function queuedAction(panelName, identifier, action) {
  const method = action === "delete" ? "DELETE" : "POST";
  const data = await api(
    panelName,
    method,
    `/api/v1/containers/${encodeURIComponent(identifier)}/${action}`,
    undefined,
    CREATE_TIMEOUT
  );
  const taskId = data?.task_id || data?.id;
  const task = taskId ? await waitForTask(panelName, taskId, TASK_TIMEOUT) : data;
  return { panel: panelName, container: identifier, action, task };
}

async function waitForImage(panelName, templateId, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let last = null;
  while (Date.now() < deadline) {
    const images = await api(panelName, "GET", "/api/v1/images");
    last = (images || []).find((img) => img.id === templateId) || last;
    if (last && last.downloaded) return last;
    if (last && !last.downloading && last.error) return last;
    await new Promise((resolve) => setTimeout(resolve, 3000));
  }
  return last || { id: templateId, downloaded: false, error: "timed out waiting for image download" };
}

const server = new McpServer({ name: "clicd", version: "1.1.0" });

server.registerTool(
  "clicd_list_panels",
  {
    title: "List configured CLICD panels",
    description: "Return every configured panel (name + URL) and which one is used by default.",
    inputSchema: {},
    annotations: { readOnlyHint: true },
  },
  async () => {
    try {
      return ok({
        default_panel: DEFAULT_PANEL,
        panels: [...panels.entries()].map(([name, p]) => ({
          name,
          base_url: p.baseUrl,
          api_key_prefix: `${p.apiKey.slice(0, 13)}...`,
        })),
      });
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_ping",
  {
    title: "Check CLICD panel connectivity",
    description: "Verify the panel URL and API key. Omit panel to check every configured panel.",
    inputSchema: { panel: panelField },
    annotations: { readOnlyHint: true },
  },
  async ({ panel }) => {
    try {
      const names = panel ? [panel] : PANEL_NAMES;
      const results = {};
      for (const name of names) {
        const resolved = resolvePanel(name);
        const [auth, version] = await Promise.all([
          api(name, "GET", "/api/check-auth"),
          api(name, "GET", "/api/version"),
        ]);
        results[resolved.name] = { base_url: resolved.baseUrl, authenticated: true, version };
      }
      return ok({ default_panel: DEFAULT_PANEL, results });
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_list_templates",
  {
    title: "List creatable images",
    description:
      "List enabled AND downloaded images/templates, ready to use as template_id. " +
      "On a fresh panel this may be empty — use clicd_list_all_images + clicd_download_image first.",
    inputSchema: {
      panel: panelField,
      virtualization: z.enum(["lxc", "kvm", "all"]).optional().describe("Filter by runtime (default all)"),
    },
    annotations: { readOnlyHint: true },
  },
  async ({ panel, virtualization }) => {
    try {
      const resolved = resolvePanel(panel);
      const runtimes = !virtualization || virtualization === "all" ? ["lxc", "kvm"] : [virtualization];
      const all = [];
      for (const runtime of runtimes) {
        const images = await api(
          resolved.name,
          "GET",
          `/api/v1/images/enabled?type=${encodeURIComponent(runtime)}`
        );
        for (const img of images || []) all.push({ panel: resolved.name, ...img, type: img.type || runtime });
      }
      return ok(all);
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_list_all_images",
  {
    title: "List every registered image with download state",
    description:
      "Return all registered LXC/KVM images including downloaded/downloading/progress/error fields. " +
      "Use it to pick a template_id and see whether it still needs clicd_download_image.",
    inputSchema: { panel: panelField },
    annotations: { readOnlyHint: true },
  },
  async ({ panel }) => {
    try {
      const resolved = resolvePanel(panel);
      const images = await api(resolved.name, "GET", "/api/v1/images");
      return ok(
        (images || []).map((img) => ({
          panel: resolved.name,
          id: img.id,
          name: img.name,
          type: img.type,
          distro: img.distro,
          release: img.release,
          arch: img.arch,
          downloaded: img.downloaded,
          downloading: img.downloading,
          progress: img.progress,
          stage: img.stage,
          error: img.error,
          size_bytes: img.size_bytes,
        }))
      );
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_download_image",
  {
    title: "Download an image/template",
    description:
      "Start downloading a registered image, then (by default) wait until it finishes. " +
      "KVM images require KVM on the host; LXC templates are fetched from the distro mirror.",
    inputSchema: {
      panel: panelField,
      template_id: z.string().min(1).describe("Image ID from clicd_list_all_images"),
      wait: z.boolean().optional().describe("Wait for the download to finish (default true)"),
      timeout_seconds: z.number().int().min(10).max(7200).optional().describe("Max wait (default 1800s)"),
    },
    annotations: { destructiveHint: false },
  },
  async ({ panel, template_id, wait, timeout_seconds }) => {
    try {
      const resolved = resolvePanel(panel);
      const started = await api(resolved.name, "POST", "/api/v1/images/download", {
        template_id,
      });
      if (wait === false) return ok({ panel: resolved.name, template_id, started });
      const image = await waitForImage(
        resolved.name,
        template_id,
        (timeout_seconds || 1800) * 1000
      );
      return ok({ panel: resolved.name, template_id, started, image });
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_set_image_enabled",
  {
    title: "Enable or disable an image",
    description: "Enable/disable an image so it can (or can no longer) be picked when creating instances.",
    inputSchema: {
      panel: panelField,
      template_id: z.string().min(1).describe("Image ID"),
      enabled: z.boolean().describe("true to enable, false to hide"),
    },
    annotations: { destructiveHint: false },
  },
  async ({ panel, template_id, enabled }) => {
    try {
      const resolved = resolvePanel(panel);
      await api(resolved.name, "PUT", "/api/v1/images/toggle", { template_id, enabled });
      return ok({ panel: resolved.name, template_id, enabled });
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_list_containers",
  {
    title: "List CLICD instances",
    description: "List LXC containers and KVM virtual machines with status, IP, ports and resources.",
    inputSchema: {
      panel: panelField,
      virtualization: z.enum(["lxc", "kvm", "all"]).optional().describe("Filter by runtime (default all)"),
      running_only: z.boolean().optional().describe("Only return running instances"),
    },
    annotations: { readOnlyHint: true },
  },
  async ({ panel, virtualization, running_only }) => {
    try {
      const resolved = resolvePanel(panel);
      let list = await api(resolved.name, "GET", "/api/v1/containers");
      if (virtualization && virtualization !== "all") {
        list = list.filter(
          (c) => (c.virtualization || (c.kvm_name ? "kvm" : "lxc")) === virtualization
        );
      }
      if (running_only) list = list.filter((c) => c.status === "running");
      return ok(list.map((c) => summarizeContainer(resolved.name, c)));
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_get_container",
  {
    title: "Get one CLICD instance",
    description: "Return full details for one instance by numeric ID, UUID or exact name.",
    inputSchema: {
      panel: panelField,
      identifier: z.union([z.number().int(), z.string()]).describe("Instance ID, UUID or name"),
    },
    annotations: { readOnlyHint: true },
  },
  async ({ panel, identifier }) => {
    try {
      const resolved = resolvePanel(panel);
      const c = await findContainer(resolved.name, identifier);
      if (!c) throw new Error(`[${resolved.name}] container not found: ${identifier}`);
      return ok({ panel: resolved.name, container: c });
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_create_container",
  {
    title: "Create an LXC container or KVM VM",
    description:
      "Create one instance. Requires name and template_id. Omit virtualization to auto-detect it from the template. " +
      "Returns the created instance summary (including NAT ports and SSH credentials) when available.",
    inputSchema: { panel: panelField, ...containerSpecSchema },
    annotations: { destructiveHint: false },
  },
  async (args) => {
    try {
      const resolved = resolvePanel(args.panel);
      if (!args.virtualization) {
        args.virtualization = await resolveVirtualization(resolved.name, args.template_id);
      }
      const payload = buildCreatePayload(args);
      await api(resolved.name, "POST", "/api/v1/containers", payload, CREATE_TIMEOUT);
      const created = await findContainer(resolved.name, args.name);
      return ok({
        panel: resolved.name,
        created: true,
        request: payload,
        container: created ? summarizeContainer(resolved.name, created) : { name: args.name },
      });
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_batch_create",
  {
    title: "Create multiple LXC/KVM instances",
    description:
      "Queue several instances in one API call. Each spec uses the same fields as clicd_create_container. " +
      "Names must be unique and images must already be downloaded/enabled.",
    inputSchema: {
      panel: panelField,
      containers: z.array(z.object(containerSpecSchema)).min(1).describe("Instance specifications"),
      wait: z.boolean().optional().describe("Wait for all create tasks to finish (default true)"),
    },
    annotations: { destructiveHint: false },
  },
  async ({ panel, containers, wait }) => {
    try {
      const resolved = resolvePanel(panel);
      const specs = [];
      for (const spec of containers) {
        if (!spec.virtualization) {
          spec.virtualization = await resolveVirtualization(resolved.name, spec.template_id);
        }
        specs.push(buildCreatePayload(spec));
      }
      const data = await api(
        resolved.name,
        "POST",
        "/api/v1/batch-create",
        { containers: specs },
        CREATE_TIMEOUT
      );
      let tasks = data?.tasks || data?.task_ids || data;
      if (wait !== false && Array.isArray(tasks)) {
        const finished = [];
        for (const item of tasks) {
          const id = typeof item === "string" ? item : item.task_id || item.id;
          if (id) finished.push(await waitForTask(resolved.name, id, TASK_TIMEOUT));
        }
        if (finished.length) tasks = finished;
      }
      return ok({ panel: resolved.name, requested: specs.length, tasks });
    } catch (err) {
      return fail(err);
    }
  }
);

for (const [name, action, title] of [
  ["clicd_start_container", "start", "Start an instance"],
  ["clicd_stop_container", "stop", "Stop (power off) an instance"],
  ["clicd_restart_container", "restart", "Restart an instance"],
  ["clicd_delete_container", "delete", "Delete an instance and its disks"],
]) {
  server.registerTool(
    name,
    {
      title,
      description: `${title} by ID, UUID or name. The operation is queued and this tool waits for the task to finish.`,
      inputSchema: {
        panel: panelField,
        identifier: z.union([z.number().int(), z.string()]).describe("Instance ID, UUID or name"),
      },
      annotations: action === "delete" ? { destructiveHint: true } : { destructiveHint: false },
    },
    async ({ panel, identifier }) => {
      try {
        const resolved = resolvePanel(panel);
        return ok(await queuedAction(resolved.name, identifier, action));
      } catch (err) {
        return fail(err);
      }
    }
  );
}

server.registerTool(
  "clicd_reinstall_container",
  {
    title: "Reinstall an instance",
    description:
      "Reinstall the OS of an existing instance. template_id defaults to the current template. " +
      "Supports SSH auth override and a post-install init script. Waits for the task to finish.",
    inputSchema: {
      panel: panelField,
      identifier: z.union([z.number().int(), z.string()]).describe("Instance ID, UUID or name"),
      template_id: z.string().optional().describe("Target image ID (defaults to the current template)"),
      ssh_auth_mode: z.enum(["auto_password", "password", "key"]).optional(),
      ssh_password: z.string().optional(),
      ssh_public_key: z.string().optional(),
      init_script: z.string().optional().describe("Post-install shell commands"),
      wait: z.boolean().optional().describe("Wait for the reinstall task (default true)"),
    },
    annotations: { destructiveHint: true },
  },
  async ({ panel, identifier, wait, ...rest }) => {
    try {
      const resolved = resolvePanel(panel);
      const body = {};
      for (const [key, value] of Object.entries(rest)) {
        if (value !== undefined && value !== "") body[key] = value;
      }
      const data = await api(
        resolved.name,
        "POST",
        `/api/v1/containers/${encodeURIComponent(identifier)}/reinstall`,
        body,
        CREATE_TIMEOUT
      );
      const taskId = data?.task_id || data?.id;
      const task =
        wait !== false && taskId ? await waitForTask(resolved.name, taskId, TASK_TIMEOUT) : data;
      return ok({ panel: resolved.name, container: identifier, action: "reinstall", task });
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_reset_password",
  {
    title: "Reset an instance SSH password",
    description: "Reset the root/administrator password. Omit password to generate a random one; the result contains the new password.",
    inputSchema: {
      panel: panelField,
      identifier: z.union([z.number().int(), z.string()]).describe("Instance ID, UUID or name"),
      password: z.string().optional().describe("Custom password; generated when omitted"),
    },
    annotations: { destructiveHint: false },
  },
  async ({ panel, identifier, password }) => {
    try {
      const resolved = resolvePanel(panel);
      const data = await api(
        resolved.name,
        "POST",
        `/api/v1/containers/${encodeURIComponent(identifier)}/reset-password`,
        password ? { password } : {}
      );
      return ok({ panel: resolved.name, ...data });
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_get_usage",
  {
    title: "Get live instance resource usage",
    description: "Return CPU, RAM, disk and network usage for one instance.",
    inputSchema: {
      panel: panelField,
      identifier: z.union([z.number().int(), z.string()]).describe("Instance ID, UUID or name"),
    },
    annotations: { readOnlyHint: true },
  },
  async ({ panel, identifier }) => {
    try {
      const resolved = resolvePanel(panel);
      return ok(
        await api(
          resolved.name,
          "GET",
          `/api/v1/containers/${encodeURIComponent(identifier)}/usage`,
          undefined,
          15000
        )
      );
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_list_storage",
  {
    title: "List storage pools and free space",
    description: "Return configured storage pools/disks with capacity, free space and per-content usage. Use pool IDs when creating instances.",
    inputSchema: { panel: panelField },
    annotations: { readOnlyHint: true },
  },
  async ({ panel }) => {
    try {
      const resolved = resolvePanel(panel);
      return ok(await api(resolved.name, "GET", "/api/v1/storage"));
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_list_routing",
  {
    title: "Get NAT/IP routing allocation",
    description: "Return NAT port range, public IPv4 pool, IPv6 prefixes and current NAT allocations.",
    inputSchema: { panel: panelField },
    annotations: { readOnlyHint: true },
  },
  async ({ panel }) => {
    try {
      const resolved = resolvePanel(panel);
      return ok(await api(resolved.name, "GET", "/api/v1/routing"));
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_host_info",
  {
    title: "Get host capacity and dashboard stats",
    description: "Return host CPU/RAM/disk capacity plus instance counts, useful before creating instances.",
    inputSchema: { panel: panelField },
    annotations: { readOnlyHint: true },
  },
  async ({ panel }) => {
    try {
      const resolved = resolvePanel(panel);
      const [dashboard, host] = await Promise.all([
        api(resolved.name, "GET", "/api/v1/dashboard"),
        api(resolved.name, "GET", "/api/v1/host-info"),
      ]);
      return ok({ panel: resolved.name, dashboard, host });
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_list_snapshots",
  {
    title: "List snapshots of an instance",
    description: "Return all snapshots for one instance, including quota and schedule.",
    inputSchema: {
      panel: panelField,
      identifier: z.union([z.number().int(), z.string()]).describe("Instance ID, UUID or name"),
    },
    annotations: { readOnlyHint: true },
  },
  async ({ panel, identifier }) => {
    try {
      const resolved = resolvePanel(panel);
      return ok(
        await api(
          resolved.name,
          "GET",
          `/api/v1/containers/${encodeURIComponent(identifier)}/snapshots`
        )
      );
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_create_snapshot",
  {
    title: "Create a snapshot",
    description: "Create a snapshot of one instance, optionally with a note and a target storage pool ID.",
    inputSchema: {
      panel: panelField,
      identifier: z.union([z.number().int(), z.string()]).describe("Instance ID, UUID or name"),
      description: z.string().optional().describe("Snapshot note"),
      storage_pool_id: z.string().optional().describe("Snapshot storage pool ID"),
    },
    annotations: { destructiveHint: false },
  },
  async ({ panel, identifier, description, storage_pool_id }) => {
    try {
      const resolved = resolvePanel(panel);
      const body = {};
      if (description) body.description = description;
      if (storage_pool_id) body.storage_pool_id = storage_pool_id;
      return ok(
        await api(
          resolved.name,
          "POST",
          `/api/v1/containers/${encodeURIComponent(identifier)}/snapshots`,
          body,
          CREATE_TIMEOUT
        )
      );
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_restore_snapshot",
  {
    title: "Restore a snapshot",
    description: "Restore an instance to one of its snapshots. Existing data is overwritten.",
    inputSchema: {
      panel: panelField,
      identifier: z.union([z.number().int(), z.string()]).describe("Instance ID, UUID or name"),
      snapshot_id: z.string().min(1).describe("Snapshot ID from clicd_list_snapshots"),
      source: z.enum(["local", "remote"]).optional().describe("Restore from local or remote copy (default local)"),
    },
    annotations: { destructiveHint: true },
  },
  async ({ panel, identifier, snapshot_id, source }) => {
    try {
      const resolved = resolvePanel(panel);
      const body = source ? { source } : {};
      return ok(
        await api(
          resolved.name,
          "POST",
          `/api/v1/containers/${encodeURIComponent(identifier)}/snapshots/${encodeURIComponent(snapshot_id)}/restore`,
          body,
          CREATE_TIMEOUT
        )
      );
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_delete_snapshot",
  {
    title: "Delete a snapshot",
    description: "Delete one snapshot of an instance and free its unique blocks.",
    inputSchema: {
      panel: panelField,
      identifier: z.union([z.number().int(), z.string()]).describe("Instance ID, UUID or name"),
      snapshot_id: z.string().min(1).describe("Snapshot ID"),
    },
    annotations: { destructiveHint: true },
  },
  async ({ panel, identifier, snapshot_id }) => {
    try {
      const resolved = resolvePanel(panel);
      return ok(
        await api(
          resolved.name,
          "DELETE",
          `/api/v1/containers/${encodeURIComponent(identifier)}/snapshots/${encodeURIComponent(snapshot_id)}`,
          undefined,
          CREATE_TIMEOUT
        )
      );
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_list_tasks",
  {
    title: "List background tasks",
    description: "Return the task queue with status, stage and errors.",
    inputSchema: {
      panel: panelField,
      status: z.enum(["pending", "running", "done", "failed"]).optional().describe("Filter by status"),
    },
    annotations: { readOnlyHint: true },
  },
  async ({ panel, status }) => {
    try {
      const resolved = resolvePanel(panel);
      let tasks = await api(resolved.name, "GET", "/api/v1/tasks");
      if (status) tasks = tasks.filter((t) => t.status === status);
      return ok({ panel: resolved.name, tasks });
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_wait_task",
  {
    title: "Wait for a task to finish",
    description: "Poll one background task until it is done or failed.",
    inputSchema: {
      panel: panelField,
      task_id: z.string().min(1).describe("Task ID"),
      timeout_seconds: z.number().int().min(5).max(7200).optional().describe("Max wait (default 900s)"),
    },
    annotations: { readOnlyHint: true },
  },
  async ({ panel, task_id, timeout_seconds }) => {
    try {
      const resolved = resolvePanel(panel);
      return ok(await waitForTask(resolved.name, task_id, (timeout_seconds || 900) * 1000));
    } catch (err) {
      return fail(err);
    }
  }
);

server.registerTool(
  "clicd_list_kvm_media",
  {
    title: "List attachable KVM ISO images",
    description: "Return ISO paths available on the host for boot_media / extra_iso.",
    inputSchema: { panel: panelField },
    annotations: { readOnlyHint: true },
  },
  async ({ panel }) => {
    try {
      const resolved = resolvePanel(panel);
      return ok(await api(resolved.name, "GET", "/api/v1/kvm-media"));
    } catch (err) {
      return fail(err);
    }
  }
);

const transport = new StdioServerTransport();
await server.connect(transport);
console.error(
  `clicd-mcp connected: ${PANEL_NAMES.map((n) => `${n}=${panels.get(n).baseUrl}`).join(", ")} (default ${DEFAULT_PANEL})`
);
