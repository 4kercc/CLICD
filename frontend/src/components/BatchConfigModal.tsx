import { useState } from 'react'
import { Check, Copy, Sliders, X } from 'lucide-react'
import { useDialog } from './Dialog'
import {
  Container,
  batchUpdateConfig,
  BatchConfigRequest,
  BatchConfigResultDetail,
} from '../services/api'
import { generateSSHPassword, sshPasswordError } from '../utils/sshAuth'

interface BatchConfigModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  selectedContainers: Container[]
}

export default function BatchConfigModal({
  isOpen,
  onClose,
  onSuccess,
  selectedContainers,
}: BatchConfigModalProps) {
  const dialog = useDialog()
  const [saving, setSaving] = useState(false)
  const [resultDetails, setResultDetails] = useState<BatchConfigResultDetail[] | null>(null)
  const [copiedMap, setCopiedMap] = useState<Record<number, boolean>>({})

  // Apply checkboxes
  const [applyVCPU, setApplyVCPU] = useState(false)
  const [vcpu, setVCPU] = useState(1)

  const [applyRAM, setApplyRAM] = useState(false)
  const [ramMB, setRAMMB] = useState(1024)

  const [applyNetworkBW, setApplyNetworkBW] = useState(false)
  const [networkDownMbps, setNetworkDownMbps] = useState(0)
  const [networkUpMbps, setNetworkUpMbps] = useState(0)

  const [applyIOSpeed, setApplyIOSpeed] = useState(false)
  const [ioReadMBps, setIOReadMBps] = useState(0)
  const [ioWriteMBps, setIOWriteMBps] = useState(0)

  const [applyTrafficLimit, setApplyTrafficLimit] = useState(false)
  const [trafficMode, setTrafficMode] = useState<'total' | 'in_out'>('total')
  const [monthlyTrafficGB, setMonthlyTrafficGB] = useState(0)
  const [trafficInGB, setTrafficInGB] = useState(0)
  const [trafficOutGB, setTrafficOutGB] = useState(0)
  const [resetTraffic, setResetTraffic] = useState(false)

  const [applyExpiresAt, setApplyExpiresAt] = useState(false)
  const [expiresAt, setExpiresAt] = useState('')

  const [applyNATQuota, setApplyNATQuota] = useState(false)
  const [natQuota, setNATQuota] = useState(2)

  const [applySnapshotQuota, setApplySnapshotQuota] = useState(false)
  const [snapshotQuota, setSnapshotQuota] = useState(2)

  const [applyPassword, setApplyPassword] = useState(false)
  const [passwordMode, setPasswordMode] = useState<'random' | 'custom'>('random')
  const [customPassword, setCustomPassword] = useState('')

  if (!isOpen) return null

  const handleCopy = (id: number, text: string) => {
    navigator.clipboard.writeText(text)
    setCopiedMap((prev) => ({ ...prev, [id]: true }))
    setTimeout(() => {
      setCopiedMap((prev) => ({ ...prev, [id]: false }))
    }, 2000)
  }

  const handleCopyAllPasswords = () => {
    if (!resultDetails) return
    const text = resultDetails
      .filter((d) => d.new_password)
      .map((d) => `${d.container_name}: ${d.new_password}`)
      .join('\n')
    navigator.clipboard.writeText(text)
    dialog.alert('已复制', '全部重置密码已复制到剪贴板')
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    const hasAnyApply =
      applyVCPU ||
      applyRAM ||
      applyNetworkBW ||
      applyIOSpeed ||
      applyTrafficLimit ||
      resetTraffic ||
      applyExpiresAt ||
      applyNATQuota ||
      applySnapshotQuota ||
      applyPassword

    if (!hasAnyApply) {
      dialog.alert('提示', '请至少勾选一项要批量调整的配置项')
      return
    }

    if (applyPassword && passwordMode === 'custom') {
      const err = sshPasswordError(customPassword)
      if (err) {
        dialog.alert('密码格式错误', err)
        return
      }
    }

    const payload: BatchConfigRequest = {
      containers: selectedContainers.map((c) => c.id),
      apply_vcpu: applyVCPU,
      vcpu: Number(vcpu),
      apply_ram: applyRAM,
      ram_mb: Number(ramMB),
      apply_network_bw: applyNetworkBW,
      network_down_mbps: Number(networkDownMbps),
      network_up_mbps: Number(networkUpMbps),
      apply_io_speed: applyIOSpeed,
      io_read_mbps: Number(ioReadMBps),
      io_write_mbps: Number(ioWriteMBps),
      apply_traffic_limit: applyTrafficLimit,
      traffic_mode: trafficMode,
      monthly_traffic_gb: Number(monthlyTrafficGB),
      traffic_in_gb: Number(trafficInGB),
      traffic_out_gb: Number(trafficOutGB),
      reset_traffic: resetTraffic,
      apply_expires_at: applyExpiresAt,
      expires_at: expiresAt,
      apply_nat_quota: applyNATQuota,
      nat_quota: Number(natQuota),
      apply_snapshot_quota: applySnapshotQuota,
      snapshot_quota: Number(snapshotQuota),
      apply_password: applyPassword,
      password_mode: passwordMode,
      password: customPassword,
    }

    setSaving(true)
    try {
      const res = await batchUpdateConfig(payload)
      const data = res.data.data
      if (data) {
        setResultDetails(data.details || [])
        if (data.failed_count === 0 && !applyPassword) {
          dialog.alert('成功', `已成功批量调整 ${data.success_count} 台容器配置`)
          onSuccess()
          onClose()
        }
      }
    } catch (err: any) {
      dialog.alert('批量修改失败', err?.response?.data?.message || '请求发生错误')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-gray-200 px-6 py-4">
          <div className="flex items-center gap-2">
            <Sliders className="h-5 w-5 text-black" />
            <div>
              <h2 className="text-base font-semibold text-black">批量调整配置</h2>
              <p className="text-xs text-gray-500">已选中 {selectedContainers.length} 台容器/虚拟机（仅勾选的项目会被更新覆盖）</p>
            </div>
          </div>
          <button onClick={onClose} className="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-black">
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Content */}
        <div className="overflow-y-auto p-6">
          {resultDetails ? (
            <div className="space-y-4">
              <div className="rounded-md bg-green-50 p-3 text-xs text-green-800">
                批量调整已完成！共成功 {resultDetails.filter((d) => d.success).length} 台，失败 {resultDetails.filter((d) => !d.success).length} 台。
              </div>

              {resultDetails.some((d) => d.new_password) && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-semibold text-gray-700">重置后的 SSH 密码列表：</span>
                    <button
                      onClick={handleCopyAllPasswords}
                      className="inline-flex items-center gap-1 text-xs text-blue-600 hover:underline"
                    >
                      <Copy className="h-3.5 w-3.5" /> 复制全部
                    </button>
                  </div>
                  <div className="max-h-60 overflow-y-auto rounded-md border border-gray-200 bg-gray-50 p-3 font-mono text-xs">
                    {resultDetails
                      .filter((d) => d.new_password)
                      .map((d) => (
                        <div key={d.container_id} className="flex items-center justify-between py-1 border-b border-gray-100 last:border-0">
                          <span className="text-gray-700 font-medium">{d.container_name}:</span>
                          <div className="flex items-center gap-2">
                            <span className="text-black select-all">{d.new_password}</span>
                            <button
                              onClick={() => handleCopy(d.container_id, d.new_password!)}
                              className="text-gray-400 hover:text-black"
                              title="复制"
                            >
                              {copiedMap[d.container_id] ? <Check className="h-3.5 w-3.5 text-green-600" /> : <Copy className="h-3.5 w-3.5" />}
                            </button>
                          </div>
                        </div>
                      ))}
                  </div>
                </div>
              )}

              <div className="flex justify-end pt-2">
                <button
                  onClick={() => {
                    onSuccess()
                    onClose()
                  }}
                  className="rounded-md bg-black px-4 py-2 text-xs font-medium text-white hover:bg-gray-800"
                >
                  关闭
                </button>
              </div>
            </div>
          ) : (
            <form onSubmit={handleSubmit} className="space-y-6">
              {/* Selected tags */}
              <div className="flex flex-wrap gap-1.5 rounded-md bg-gray-50 p-2.5 max-h-24 overflow-y-auto border border-gray-200">
                {selectedContainers.map((c) => (
                  <span key={c.id} className="inline-flex items-center rounded bg-white px-2 py-0.5 text-xs text-gray-700 border border-gray-200">
                    {c.name} <span className="ml-1 text-[10px] text-gray-400">({c.virtualization?.toUpperCase() || 'LXC'})</span>
                  </span>
                ))}
              </div>

              {/* 1. Computing Resources */}
              <div className="rounded-lg border border-gray-200 p-4 space-y-3">
                <h3 className="text-xs font-bold uppercase tracking-wider text-gray-500">计算资源限制</h3>
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <label className="flex items-center gap-2 text-xs font-medium text-gray-700">
                      <input
                        type="checkbox"
                        checked={applyVCPU}
                        onChange={(e) => setApplyVCPU(e.target.checked)}
                        className="rounded border-gray-300 accent-black"
                      />
                      调整 CPU 核心数 (vCPU)
                    </label>
                    <input
                      type="number"
                      step="0.1"
                      min="0.1"
                      disabled={!applyVCPU}
                      value={vcpu}
                      onChange={(e) => setVCPU(Number(e.target.value))}
                      className="w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black disabled:bg-gray-100 disabled:text-gray-400"
                    />
                  </div>
                  <div className="space-y-1.5">
                    <label className="flex items-center gap-2 text-xs font-medium text-gray-700">
                      <input
                        type="checkbox"
                        checked={applyRAM}
                        onChange={(e) => setApplyRAM(e.target.checked)}
                        className="rounded border-gray-300 accent-black"
                      />
                      调整内存大小 (MB)
                    </label>
                    <input
                      type="number"
                      min="64"
                      disabled={!applyRAM}
                      value={ramMB}
                      onChange={(e) => setRAMMB(Number(e.target.value))}
                      className="w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black disabled:bg-gray-100 disabled:text-gray-400"
                    />
                  </div>
                </div>
              </div>

              {/* 2. Bandwidth & IO Limits */}
              <div className="rounded-lg border border-gray-200 p-4 space-y-3">
                <h3 className="text-xs font-bold uppercase tracking-wider text-gray-500">网络与磁盘速率 (0 表示不限速)</h3>
                <div className="space-y-3">
                  <div className="space-y-1.5">
                    <label className="flex items-center gap-2 text-xs font-medium text-gray-700">
                      <input
                        type="checkbox"
                        checked={applyNetworkBW}
                        onChange={(e) => setApplyNetworkBW(e.target.checked)}
                        className="rounded border-gray-300 accent-black"
                      />
                      调整网络带宽限速 (Mbps)
                    </label>
                    <div className="grid grid-cols-2 gap-2">
                      <input
                        type="number"
                        min="0"
                        placeholder="下行 (入站) Mbps"
                        disabled={!applyNetworkBW}
                        value={networkDownMbps}
                        onChange={(e) => setNetworkDownMbps(Number(e.target.value))}
                        className="w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black disabled:bg-gray-100 disabled:text-gray-400"
                      />
                      <input
                        type="number"
                        min="0"
                        placeholder="上行 (出站) Mbps"
                        disabled={!applyNetworkBW}
                        value={networkUpMbps}
                        onChange={(e) => setNetworkUpMbps(Number(e.target.value))}
                        className="w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black disabled:bg-gray-100 disabled:text-gray-400"
                      />
                    </div>
                  </div>

                  <div className="space-y-1.5">
                    <label className="flex items-center gap-2 text-xs font-medium text-gray-700">
                      <input
                        type="checkbox"
                        checked={applyIOSpeed}
                        onChange={(e) => setApplyIOSpeed(e.target.checked)}
                        className="rounded border-gray-300 accent-black"
                      />
                      调整磁盘 I/O 速率限速 (MB/s)
                    </label>
                    <div className="grid grid-cols-2 gap-2">
                      <input
                        type="number"
                        min="0"
                        placeholder="读速度 (MB/s)"
                        disabled={!applyIOSpeed}
                        value={ioReadMBps}
                        onChange={(e) => setIOReadMBps(Number(e.target.value))}
                        className="w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black disabled:bg-gray-100 disabled:text-gray-400"
                      />
                      <input
                        type="number"
                        min="0"
                        placeholder="写速度 (MB/s)"
                        disabled={!applyIOSpeed}
                        value={ioWriteMBps}
                        onChange={(e) => setIOWriteMBps(Number(e.target.value))}
                        className="w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black disabled:bg-gray-100 disabled:text-gray-400"
                      />
                    </div>
                  </div>
                </div>
              </div>

              {/* 3. Traffic Limits & Reset */}
              <div className="rounded-lg border border-gray-200 p-4 space-y-3">
                <h3 className="text-xs font-bold uppercase tracking-wider text-gray-500">流量配额控制 (0 表示无限制)</h3>
                <div className="space-y-3">
                  <div className="space-y-1.5">
                    <label className="flex items-center gap-2 text-xs font-medium text-gray-700">
                      <input
                        type="checkbox"
                        checked={applyTrafficLimit}
                        onChange={(e) => setApplyTrafficLimit(e.target.checked)}
                        className="rounded border-gray-300 accent-black"
                      />
                      调整月度流量限制
                    </label>
                    {applyTrafficLimit && (
                      <div className="space-y-2 pt-1">
                        <div className="flex gap-4">
                          <label className="flex items-center gap-1.5 text-xs text-gray-700">
                            <input
                              type="radio"
                              name="trafficMode"
                              checked={trafficMode === 'total'}
                              onChange={() => setTrafficMode('total')}
                              className="accent-black"
                            />
                            总流量模式 (双向合计)
                          </label>
                          <label className="flex items-center gap-1.5 text-xs text-gray-700">
                            <input
                              type="radio"
                              name="trafficMode"
                              checked={trafficMode === 'in_out'}
                              onChange={() => setTrafficMode('in_out')}
                              className="accent-black"
                            />
                            上下行独立模式
                          </label>
                        </div>
                        {trafficMode === 'total' ? (
                          <input
                            type="number"
                            min="0"
                            placeholder="每月总流量上限 (GB)"
                            value={monthlyTrafficGB}
                            onChange={(e) => setMonthlyTrafficGB(Number(e.target.value))}
                            className="w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black"
                          />
                        ) : (
                          <div className="grid grid-cols-2 gap-2">
                            <input
                              type="number"
                              min="0"
                              placeholder="入站 (下载) GB"
                              value={trafficInGB}
                              onChange={(e) => setTrafficInGB(Number(e.target.value))}
                              className="w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black"
                            />
                            <input
                              type="number"
                              min="0"
                              placeholder="出站 (上传) GB"
                              value={trafficOutGB}
                              onChange={(e) => setTrafficOutGB(Number(e.target.value))}
                              className="w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black"
                            />
                          </div>
                        )}
                      </div>
                    )}
                  </div>

                  <label className="flex items-center gap-2 text-xs font-medium text-red-600 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={resetTraffic}
                      onChange={(e) => setResetTraffic(e.target.checked)}
                      className="rounded border-gray-300 accent-red-600"
                    />
                    立即清零并重置本月已用流量统计 (RX / TX 归零)
                  </label>
                </div>
              </div>

              {/* 4. Expiry & Quotas */}
              <div className="rounded-lg border border-gray-200 p-4 space-y-3">
                <h3 className="text-xs font-bold uppercase tracking-wider text-gray-500">服务周期与配额</h3>
                <div className="grid gap-3 sm:grid-cols-3">
                  <div className="space-y-1.5">
                    <label className="flex items-center gap-2 text-xs font-medium text-gray-700">
                      <input
                        type="checkbox"
                        checked={applyExpiresAt}
                        onChange={(e) => setApplyExpiresAt(e.target.checked)}
                        className="rounded border-gray-300 accent-black"
                      />
                      到期时间
                    </label>
                    <input
                      type="datetime-local"
                      disabled={!applyExpiresAt}
                      value={expiresAt ? expiresAt.replace(' ', 'T').slice(0, 16) : ''}
                      onChange={(e) => setExpiresAt(e.target.value ? e.target.value.replace('T', ' ') + ':00' : '')}
                      className="w-full rounded-md border border-gray-300 px-2 py-1.5 text-xs text-black disabled:bg-gray-100 disabled:text-gray-400"
                    />
                    {applyExpiresAt && (
                      <button
                        type="button"
                        onClick={() => setExpiresAt('')}
                        className="text-[11px] text-blue-600 hover:underline"
                      >
                        设为永不到期 (清空)
                      </button>
                    )}
                  </div>

                  <div className="space-y-1.5">
                    <label className="flex items-center gap-2 text-xs font-medium text-gray-700">
                      <input
                        type="checkbox"
                        checked={applyNATQuota}
                        onChange={(e) => setApplyNATQuota(e.target.checked)}
                        className="rounded border-gray-300 accent-black"
                      />
                      NAT 端口配额
                    </label>
                    <input
                      type="number"
                      min="0"
                      max="999"
                      disabled={!applyNATQuota}
                      value={natQuota}
                      onChange={(e) => setNATQuota(Number(e.target.value))}
                      className="w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black disabled:bg-gray-100 disabled:text-gray-400"
                    />
                  </div>

                  <div className="space-y-1.5">
                    <label className="flex items-center gap-2 text-xs font-medium text-gray-700">
                      <input
                        type="checkbox"
                        checked={applySnapshotQuota}
                        onChange={(e) => setApplySnapshotQuota(e.target.checked)}
                        className="rounded border-gray-300 accent-black"
                      />
                      快照保留上限
                    </label>
                    <input
                      type="number"
                      min="1"
                      disabled={!applySnapshotQuota}
                      value={snapshotQuota}
                      onChange={(e) => setSnapshotQuota(Number(e.target.value))}
                      className="w-full rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black disabled:bg-gray-100 disabled:text-gray-400"
                    />
                  </div>
                </div>
              </div>

              {/* 5. SSH Password */}
              <div className="rounded-lg border border-gray-200 p-4 space-y-3">
                <h3 className="text-xs font-bold uppercase tracking-wider text-gray-500">SSH 登录密码批量重置</h3>
                <div className="space-y-2">
                  <label className="flex items-center gap-2 text-xs font-medium text-gray-700">
                    <input
                      type="checkbox"
                      checked={applyPassword}
                      onChange={(e) => setApplyPassword(e.target.checked)}
                      className="rounded border-gray-300 accent-black"
                    />
                    批量重置 root 登录密码
                  </label>
                  {applyPassword && (
                    <div className="space-y-2 pt-1">
                      <div className="flex gap-4">
                        <label className="flex items-center gap-1.5 text-xs text-gray-700">
                          <input
                            type="radio"
                            name="passwordMode"
                            checked={passwordMode === 'random'}
                            onChange={() => setPasswordMode('random')}
                            className="accent-black"
                          />
                          每台生成独立强随机密码（推荐）
                        </label>
                        <label className="flex items-center gap-1.5 text-xs text-gray-700">
                          <input
                            type="radio"
                            name="passwordMode"
                            checked={passwordMode === 'custom'}
                            onChange={() => setPasswordMode('custom')}
                            className="accent-black"
                          />
                          统一设置为相同密码
                        </label>
                      </div>
                      {passwordMode === 'custom' && (
                        <div className="flex gap-2">
                          <input
                            type="text"
                            placeholder="输入自定义强密码（8-64位，含字母数字）"
                            value={customPassword}
                            onChange={(e) => setCustomPassword(e.target.value)}
                            className="flex-1 rounded-md border border-gray-300 px-3 py-1.5 text-xs text-black"
                          />
                          <button
                            type="button"
                            onClick={() => setCustomPassword(generateSSHPassword())}
                            className="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50 whitespace-nowrap"
                          >
                            随机生成
                          </button>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              </div>

              {/* Footer Buttons */}
              <div className="flex items-center justify-end gap-2 pt-2 border-t border-gray-200">
                <button
                  type="button"
                  onClick={onClose}
                  disabled={saving}
                  className="rounded-md border border-gray-300 px-4 py-2 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={saving}
                  className="inline-flex items-center gap-1.5 rounded-md bg-black px-4 py-2 text-xs font-medium text-white hover:bg-gray-800 disabled:opacity-50"
                >
                  {saving ? '正在批量处理...' : '确认批量调整'}
                </button>
              </div>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}
