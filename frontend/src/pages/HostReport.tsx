import { ReactNode, useCallback, useEffect, useState } from 'react'
import {
  Activity,
  CheckCircle2,
  Cpu,
  HardDrive,
  MemoryStick,
  RefreshCw,
  XCircle,
} from 'lucide-react'
import { getHostReport, HostProbeReport } from '../services/api'

export default function HostReport() {
  const [report, setReport] = useState<HostProbeReport | null>(null)
  const [loading, setLoading] = useState(true)

  const fetchReport = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getHostReport()
      setReport(res.data.data || null)
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchReport()
  }, [fetchReport])

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-black">宿主机信息</h1>
          <p className="mt-1 text-sm text-gray-500">硬件、网络、磁盘健康与运行环境探测报告</p>
        </div>
        <button onClick={fetchReport} disabled={loading} className="inline-flex items-center gap-1.5 rounded-md border border-gray-200 px-3 py-2 text-sm text-gray-600 hover:bg-gray-50 disabled:opacity-50">
          <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          刷新
        </button>
      </div>

      {loading && !report ? (
        <div className="rounded-lg border border-gray-200 bg-white py-14 text-center text-sm text-gray-400">正在探测宿主机环境...</div>
      ) : !report ? (
        <div className="rounded-lg border border-gray-200 bg-white py-14 text-center text-sm text-gray-400">暂未获取到宿主机信息</div>
      ) : (
        <div className="space-y-5">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <ProbeMetric icon={<Cpu className="h-4 w-4" />} label="CPU" value={report.cpu.model || 'Unknown'} sub={`${report.cpu.cores} 核 / ${report.cpu.threads} 线程`} />
            <ProbeMetric icon={<MemoryStick className="h-4 w-4" />} label="RAM" value={formatMB(report.memory.total_mb)} sub={`${formatMB(report.memory.used_mb)} 已用`} />
            <ProbeMetric icon={<HardDrive className="h-4 w-4" />} label="DISK" value={`${report.disks.length} 块硬盘`} sub={report.disks.map(d => d.type).filter(Boolean).join(' / ') || 'Unknown'} />
            <ProbeMetric icon={<Activity className="h-4 w-4" />} label="运行状态" value={report.system.uptime_text} sub={`${report.system.process_count} 个进程`} />
          </div>

          <ProbeSection title="系统概览">
            <ProbeRows rows={[
              ['主机名', report.hostname],
              ['操作系统', report.os],
              ['内核', report.kernel],
              ['生成时间', report.generated_at],
              ['CPU 架构', report.cpu.architecture],
              ['CPU 虚拟化指令', report.cpu.virtualization ? `支持 (${report.cpu.virtualization_key})` : '未检测到'],
              ['CPU 核显', report.cpu.has_integrated_gpu ? '检测到' : '未检测到'],
              ['显卡', report.gpus.length ? `${report.gpus.length} 个` : '未检测到'],
              ['运行能力', runtimeModeLabel(report.runtime.support_mode)],
              ['KVM 嵌套虚拟化', `${report.runtime.nested_virtualization ? '支持' : '未检测到'} (${report.runtime.nested_detail || '-'})`],
            ]} />
          </ProbeSection>

          <ProbeSection title="公网与路由">
            <ProbeRows rows={[
              ['公网 IPv4', report.public_ipv4.length ? report.public_ipv4.join('\n') : '未检测到'],
              ['IPv4 地址', report.ipv4_addresses?.length ? report.ipv4_addresses.map(formatIPv4Address).join('\n') : '未检测到'],
              ['IPv4 段', report.ipv4_prefixes?.length ? report.ipv4_prefixes.map(formatIPv4Prefix).join('\n') : '未检测到'],
              ['IPv6 地址', report.ipv6_addresses.length ? report.ipv6_addresses.map(ip => `${ip.address}/${ip.prefix_len} (${ip.interface})`).join('\n') : '未检测到'],
              ['IPv6 段', report.ipv6_prefixes?.length ? report.ipv6_prefixes.map(formatIPv6Prefix).join('\n') : '未检测到'],
              ['网关', report.gateways.length ? report.gateways.map(g => `${g.family}: ${g.gateway || '-'} dev ${g.interface || '-'}`).join('\n') : '未检测到'],
            ]} />
          </ProbeSection>

          <ProbeTable
            title="内存条"
            empty="未检测到内存条明细，可能缺少 dmidecode 或权限受限"
            headers={['插槽', '容量', '类型', '频率', '厂商', '型号/序列号']}
            rows={(report.memory.modules || []).map(m => [
              m.locator || '-',
              m.size || '-',
              m.type || '-',
              m.speed || '-',
              m.manufacturer || '-',
              [m.part_number, m.serial_number].filter(Boolean).join(' / ') || '-',
            ])}
          />

          <ProbeTable
            title="硬盘与健康"
            empty="未检测到硬盘"
            headers={['设备', '型号', '容量', '类型', '挂载点', '健康', '寿命', '通电', '读取', '写入', '命令数', '擦写']}
            rows={report.disks.map(d => [
              `${d.path || d.name}\n${d.serial || ''}`,
              d.model || '-',
              formatBytes(d.size_bytes),
              d.type || (d.rotational ? 'HDD' : 'SSD'),
              d.mountpoints?.length ? d.mountpoints.join('\n') : '-',
              `${diskHealthLabel(d.health)}\n${d.health_detail || ''}`,
              formatLifeUsed(d.smart?.life_used_percent),
              d.smart?.power_on_hours ? `${d.smart.power_on_hours} 小时\n${formatPowerOnDays(d.smart.power_on_hours)}` : '-',
              formatBytes(d.smart?.read_data_bytes || 0),
              formatBytes(d.smart?.written_data_bytes || 0),
              formatCommands(d.smart?.read_commands, d.smart?.write_commands),
              formatWear(d.smart?.wear_leveling_count, d.smart?.erase_count, d.smart?.power_cycle_count),
            ])}
          />

          <ProbeTable
            title="网卡"
            empty="未检测到网卡"
            headers={['网卡', '状态', '驱动/速率', 'MAC', 'IPv4', 'IPv6']}
            rows={report.network_interfaces.map(n => [
              `${n.name}\n${n.model || ''}`,
              n.state || '-',
              `${n.driver || '-'}\n${n.speed_mbps > 0 ? `${n.speed_mbps} Mbps` : '-'}`,
              n.mac || '-',
              n.ipv4?.length ? n.ipv4.map(ip => `${ip.address}/${ip.prefix_len}`).join('\n') : '-',
              n.ipv6?.length ? n.ipv6.map(ip => `${ip.address}/${ip.prefix_len} ${ip.scope}`).join('\n') : '-',
            ])}
          />

          <ProbeTable
            title="显卡"
            empty="未检测到显卡"
            headers={['名称', '厂商', '类型', '驱动']}
            rows={report.gpus.map(g => [g.name, g.vendor || '-', gpuTypeLabel(g.type), g.driver || '-'])}
          />

          <ProbeSection title="环境支持">
            <div className="grid gap-2 md:grid-cols-2">
              {report.environment.map(item => (
                <div key={item.key} className="flex items-start gap-2 rounded-lg border border-gray-200 bg-white px-3 py-2">
                  {item.ok ? <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-green-600" /> : <XCircle className={`mt-0.5 h-4 w-4 shrink-0 ${item.required ? 'text-red-600' : 'text-amber-600'}`} />}
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2 text-xs font-medium text-gray-800">
                      <span>{item.label}</span>
                      <span className={`rounded px-1.5 py-0.5 text-[10px] ${item.required ? 'bg-gray-100 text-gray-600' : 'bg-blue-50 text-blue-700'}`}>
                        {item.required ? '必要' : '可选'}
                      </span>
                    </div>
                    <div className="mt-1 break-all font-mono text-[11px] text-gray-500">{item.detail || '-'}</div>
                  </div>
                </div>
              ))}
            </div>
          </ProbeSection>
        </div>
      )}
    </div>
  )
}

function ProbeMetric({ icon, label, value, sub }: { icon: ReactNode; label: string; value: string; sub: string }) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white px-3 py-3">
      <div className="mb-2 flex items-center gap-2 text-xs font-medium text-gray-500">
        {icon}
        {label}
      </div>
      <div className="line-clamp-2 break-words text-sm font-semibold text-gray-900" title={value}>{value}</div>
      <div className="mt-1 truncate text-xs text-gray-500" title={sub}>{sub}</div>
    </div>
  )
}

function ProbeSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h2 className="mb-2 text-sm font-semibold text-black">{title}</h2>
      {children}
    </section>
  )
}

function ProbeRows({ rows }: { rows: Array<[string, string]> }) {
  return (
    <div className="overflow-hidden rounded-lg border border-gray-200 bg-white">
      {rows.map(([label, value]) => (
        <div key={label} className="grid gap-2 border-b border-gray-100 px-3 py-2 text-xs last:border-b-0 md:grid-cols-[160px_1fr]">
          <div className="font-medium text-gray-500">{label}</div>
          <div className="whitespace-pre-wrap break-words font-mono text-gray-800">{value || '-'}</div>
        </div>
      ))}
    </div>
  )
}

function ProbeTable({ title, headers, rows, empty }: { title: string; headers: string[]; rows: string[][]; empty: string }) {
  return (
    <section>
      <h2 className="mb-2 text-sm font-semibold text-black">{title}</h2>
      {rows.length === 0 ? (
        <div className="rounded-lg border border-gray-200 bg-white px-3 py-3 text-xs text-gray-400">{empty}</div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-gray-200 bg-white">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-gray-100 bg-gray-50 text-left text-gray-500">
                {headers.map(header => <th key={header} className="px-3 py-2 font-medium">{header}</th>)}
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {rows.map((row, rowIndex) => (
                <tr key={rowIndex} className="align-top">
                  {row.map((cell, cellIndex) => (
                    <td key={cellIndex} className="max-w-[280px] whitespace-pre-wrap break-words px-3 py-2 text-gray-700">
                      {cell || '-'}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function formatIPv4Address(ip: HostProbeReport['ipv4_addresses'][number]) {
  return `${ip.address}/${ip.prefix_len} (${ip.interface})`
}

function formatIPv4Prefix(prefix: HostProbeReport['ipv4_prefixes'][number]) {
  const parts = [
    prefix.prefix || '-',
    prefix.subnet_mask ? `mask ${prefix.subnet_mask}` : '',
    prefix.gateway ? `via ${prefix.gateway}` : '',
    prefix.interface ? `dev ${prefix.interface}` : '',
    prefix.source ? `[${prefix.source}]` : '',
  ].filter(Boolean)
  return parts.join(' ')
}

function formatIPv6Prefix(prefix: HostProbeReport['ipv6_prefixes'][number]) {
  const value = prefix.prefix || prefix.address || '-'
  const cidr = value.includes('/') || !prefix.prefix_len ? value : `${value}/${prefix.prefix_len}`
  return `${cidr} via ${prefix.gateway || '-'}`
}

function formatMB(value: number) {
  if (!value) return '-'
  if (value >= 1024) return `${(value / 1024).toFixed(1)} GB`
  return `${value} MB`
}

function formatBytes(value: number) {
  if (!value) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let next = value
  let index = 0
  while (next >= 1024 && index < units.length - 1) {
    next /= 1024
    index++
  }
  return `${next.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function formatLifeUsed(value?: number) {
  if (value === undefined || value === null) return '-'
  return `${value}% 已用\n${Math.max(0, 100 - value)}% 剩余`
}

function formatPowerOnDays(hours: number) {
  const days = Math.floor(hours / 24)
  const rest = hours % 24
  return days > 0 ? `${days} 天 ${rest} 小时` : `${hours} 小时`
}

function formatCommands(read?: number, write?: number) {
  if (!read && !write) return '-'
  return `读 ${formatCount(read || 0)}\n写 ${formatCount(write || 0)}`
}

function formatCount(value: number) {
  if (!value) return '-'
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(1)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return `${value}`
}

function formatWear(wear?: string, erase?: string, powerCycles?: number) {
  const rows: string[] = []
  if (wear) rows.push(`磨损 ${wear}`)
  if (erase) rows.push(`擦写 ${erase}`)
  if (powerCycles) rows.push(`启停 ${powerCycles}`)
  return rows.length ? rows.join('\n') : '-'
}

function runtimeModeLabel(value: string) {
  switch (value) {
    case 'kvm_lxc':
      return '支持 KVM + LXC'
    case 'lxc_only':
      return '仅支持 LXC'
    default:
      return '未满足运行环境'
  }
}

function diskHealthLabel(value: string) {
  switch (value) {
    case 'ok':
      return '健康'
    case 'failed':
      return '异常'
    default:
      return '未知'
  }
}

function gpuTypeLabel(value: string) {
  if (value === 'integrated') return '核显'
  if (value === 'discrete') return '独显'
  return value || '-'
}
