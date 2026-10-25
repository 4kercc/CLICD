// Smoke test: spawn the CLICD MCP server and call a few read-only tools.
// Usage:
//   CLICD_BASE_URL=http://host:8999 CLICD_API_KEY=clicd_sk_... node test-client.mjs
// or rely on clicd-mcp.local.json.
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const transport = new StdioClientTransport({
  command: process.execPath,
  args: [join(here, "server.js")],
  env: { ...process.env },
  cwd: here,
});

const client = new Client({ name: "clicd-mcp-smoke", version: "1.0.0" });
await client.connect(transport);

const text = (r) => r.content?.[0]?.text || "";
const call = async (tool, args) => {
  const r = await client.callTool({ name: tool, arguments: args });
  return { isError: r.isError, text: text(r) };
};

const { tools } = await client.listTools();
console.log("TOOLS:", tools.map((t) => t.name).join(", "));

console.log("\nPANELS:", (await call("clicd_list_panels", {})).text.replace(/\s+/g, " "));
console.log("PING:", (await call("clicd_ping", {})).text.replace(/\s+/g, " ").slice(0, 400));

for (const panel of ["独服", "绿云"]) {
  const list = await call("clicd_list_containers", { panel });
  if (list.isError) {
    console.log(`${panel} CONTAINERS ERROR:`, list.text);
    continue;
  }
  const parsed = JSON.parse(list.text);
  console.log(
    `${panel} CONTAINERS:`,
    parsed.length,
    parsed.map((c) => `${c.id}:${c.name}:${c.status}`).join(" ")
  );
}

const images = await call("clicd_list_all_images", { panel: "绿云" });
const parsedImages = JSON.parse(images.text);
console.log(
  "绿云 IMAGES:",
  parsedImages.length,
  "downloaded:",
  parsedImages.filter((i) => i.downloaded).length
);

await client.close();
