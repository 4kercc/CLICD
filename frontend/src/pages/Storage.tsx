import { useCallback, useEffect, useMemo, useState } from 'react'
import { AlertCircle, CheckCircle2, Cloud, HardDrive, Pencil, Plus, RefreshCw, Save, Trash2, ArrowUpRight } from 'lucide-react'
import { getStorageInfo, updateStoragePools, testRemoteStorage, syncAllToRemoteStorage, StorageDisk, StorageInfo, StoragePool } from '../services/api'
import { useLanguage } from '../contexts/LanguageContext'
import { useDialog } from '../components/Dialog'

const contentOptions = [
  ['lxc', 'LXC 容器'],
  ['kvm', 'KVM 磁盘'],
  ['images', '镜像缓存'],
  ['snapshots', '快照'],
  ['backups', '备份'],
] as const

const contentLabels = Object.fromEntries(contentOptions)

const contentColors: Record<string, string> = {
  lxc: '#2563eb',
  kvm: '#7c3aed',
  images: '#d97706',
  snapshots: '#059669',
  backups: '#0891b2',
}

export default function Storage() {
  const { t } = useLanguage()
  const dialog = useDialog()
  const [info, setInfo] = useState<StorageInfo | null>(null)
  const [pools, setPools] = useState<StoragePool[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [syncingPool, setSyncingPool] = useState<string | null>(null)
  const [saveMessage, setSaveMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  // Remote storage modal state
  const [showRemoteModal, setShowRemoteModal] = useState(false)
  const [editingRemoteId, setEditingRemoteId] = useState<string | null>(null)
  const [testingRemote, setTestingRemote] = useState(false)
  const [remoteTestMessage, setRemoteTestMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [remoteDraft, setRemoteDraft] = useState<{
    id: string
    name: string
    type: 'sftp' | 'webdav' | 'minio' | 'onedrive' | 'googledrive'
    sync_snapshots: boolean
    sync_backups: boolean
    config: Record<string, string>
  }>({
    id: '',
    name: '',
    type: 'sftp',
    sync_snapshots: true,
    sync_backups: true,
    config: defaultRemoteConfigFor('sftp'),
  })

  const openAddRemoteModal = () => {
    setEditingRemoteId(null)
    setRemoteDraft({
      id: '',
      name: '',
      type: 'sftp',
      sync_snapshots: true,
      sync_backups: true,
      config: defaultRemoteConfigFor('sftp'),
    })
    setRemoteTestMessage(null)
    setShowRemoteModal(true)
  }

  const openEditRemoteModal = (rp: StoragePool) => {
    setEditingRemoteId(rp.id)
    const type = (['sftp', 'webdav', 'minio', 'onedrive', 'googledrive'].includes(rp.type || '') ? rp.type : 'sftp') as
      'sftp' | 'webdav' | 'minio' | 'onedrive' | 'googledrive'
    setRemoteDraft({
      id: rp.id,
      name: rp.name,
      type,
      sync_snapshots: !!rp.sync_snapshots,
      sync_backups: !!rp.sync_backups,
      config: { ...defaultRemoteConfigFor(type), ...(rp.config || {}) },
    })
    setRemoteTestMessage(null)
    setShowRemoteModal(true)
  }

  const switchRemoteType = (t: 'sftp' | 'webdav' | 'minio' | 'onedrive' | 'googledrive') => {
    // Switching protocol resets the config so fields of other protocols do not leak in.
    setRemoteDraft({ ...remoteDraft, type: t, config: defaultRemoteConfigFor(t) })
    setRemoteTestMessage(null)
  }

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getStorageInfo()
      const data = res.data.data || { pools: [], disks: [], content_types: [] }
      setInfo(data)
      setPools(data.pools || [])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  useEffect(() => {
    if (!saveMessage) return
    const timer = window.setTimeout(() => setSaveMessage(null), 3500)
    return () => window.clearTimeout(timer)
  }, [saveMessage])

  const mountedDisks = useMemo(() => (info?.disks || []).filter((disk) => !!disk.mount_point), [info?.disks])
  const remotePools = useMemo(() => (pools || []).filter((p) => p.type && p.type !== 'local'), [pools])

  const save = async (customPools?: StoragePool[]) => {
    setSaveMessage(null)
    setSaving(true)
    try {
      const targetPools = customPools || pools
      const normalized = targetPools
        .map((pool) => ({
          ...pool,
          id: (pool.id || pool.name || '').trim(),
          name: (pool.name || '').trim(),
          type: pool.type || 'local',
          path: (pool.path || '').trim(),
          content_types: pool.content_types || [],
          default_contents: (pool.default_contents || []).filter((item) => (pool.content_types || []).includes(item)),
          enabled: pool.enabled !== false,
          sync_snapshots: !!pool.sync_snapshots,
          sync_backups: !!pool.sync_backups,
          config: pool.config || {},
        }))
      const res = await updateStoragePools(normalized)
      const data = res.data.data
      if (data) {
        setInfo(data)
        setPools(data.pools || [])
      }
      setSaveMessage({ type: 'success', text: '存储配置已保存' })
    } catch (err: any) {
      setSaveMessage({ type: 'error', text: err?.response?.data?.message || '保存存储配置失败' })
    } finally {
      setSaving(false)
    }
  }

  const handleTestRemote = async () => {
    setRemoteTestMessage(null)
    setTestingRemote(true)
    try {
      const res = await testRemoteStorage(remoteDraft.type, remoteDraft.config)
      setRemoteTestMessage({ type: 'success', text: res.data.message || '连接测试成功' })
    } catch (err: any) {
      setRemoteTestMessage({ type: 'error', text: err?.response?.data?.message || '连接失败，请检查配置' })
    } finally {
      setTestingRemote(false)
    }
  }

  const handleAddRemotePool = () => {
    if (!remoteDraft.name.trim()) {
      alert('请输入存储名称')
      return
    }
    const newPool: StoragePool = {
      id: remoteDraft.id || `remote-${remoteDraft.type}-${Date.now()}`,
      name: remoteDraft.name.trim(),
      type: remoteDraft.type,
      enabled: true,
      sync_snapshots: remoteDraft.sync_snapshots,
      sync_backups: remoteDraft.sync_backups,
      config: { ...remoteDraft.config },
      content_types: ['snapshots', 'backups'],
      default_contents: [],
    }
    const updated = [...pools.filter((p) => p.id !== newPool.id), newPool]
    setPools(updated)
    setShowRemoteModal(false)
    save(updated)
  }

  const handleDeletePool = (poolId: string) => {
    if (!confirm('确认删除该远程存储配置吗？')) return
    const updated = pools.filter((p) => p.id !== poolId)
    setPools(updated)
    save(updated)
  }

  const updateDiskPool = (disk: StorageDisk, updater: (pool: StoragePool) => StoragePool) => {
    setPools((current) => {
      const index = current.findIndex((pool) => poolForDisk(pool, disk))
      const base = index >= 0 ? current[index] : defaultPoolForDisk(disk)
      const nextPool = updater(base)
      if (index >= 0) {
        return current.map((item, i) => i === index ? nextPool : item)
      }
      return [...current, nextPool]
    })
  }

  const toggleContent = (disk: StorageDisk, content: string) => {
    updateDiskPool(disk, (pool) => {
      const current = pool.content_types || []
      const enabled = current.includes(content)
      const contentTypes = enabled ? current.filter((item) => item !== content) : [...current, content]
      return {
        ...pool,
        enabled: true,
        content_types: contentTypes,
        default_contents: (pool.default_contents || []).filter((item) => contentTypes.includes(item)),
      }
    })
  }

  const toggleDefault = (disk: StorageDisk, content: string) => {
    setPools((current) => {
      const index = current.findIndex((pool) => poolForDisk(pool, disk))
      const base = index >= 0 ? current[index] : defaultPoolForDisk(disk)
      if (!(base.content_types || []).includes(content)) return current
      const hasDefault = (base.default_contents || []).includes(content)
      const baseDefaults = (base.default_contents || []).filter((value) => value !== content)
      const cleared = current.map((item) => ({
        ...item,
        default_contents: (item.default_contents || []).filter((value) => value !== content),
      }))
      const nextPool = {
        ...base,
        default_contents: hasDefault ? baseDefaults : [...baseDefaults, content],
      }
      if (index >= 0) {
        return cleared.map((item, i) => i === index ? nextPool : item)
      }
      return [...cleared, nextPool]
    })
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-black"></div>
      </div>
    )
  }

  return (
    <div className="min-w-0 space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold text-black dark:text-white">{t('存储管理')}</h1>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">{t('支持管理宿主机本地挂载磁盘与远程对象存储/文件系统（SFTP / WebDAV / MinIO S3），支持快照/备份异地双写同步。')}</p>
        </div>
        <div className="flex shrink-0 gap-2 flex-wrap items-center">
          {remotePools.length > 0 && (
            <button
              onClick={async () => {
                if (!(await dialog.confirm('一键全量同步', '确定将系统中所有虚拟机和容器现有的历史快照与全量备份同步至启用的远程存储吗？这将在后台按需上传文件。'))) return
                setSyncingPool('all')
                try {
                  const res = await syncAllToRemoteStorage()
                  dialog.alert('同步完成', res.data.message || '已成功触发全量快照与备份同步。')
                  fetchData()
                } catch (err: unknown) {
                  const error = err as { response?: { data?: { message?: string } } }
                  dialog.alert('同步失败', error.response?.data?.message || '同步至远程存储失败，请检查连通性')
                } finally {
                  setSyncingPool(null)
                }
              }}
              disabled={!!syncingPool}
              className="inline-flex items-center gap-1.5 rounded-md border border-emerald-600 bg-emerald-50 px-3 py-2 text-sm font-medium text-emerald-700 hover:bg-emerald-100 disabled:opacity-50"
              title="将系统中所有已创建的快照与全量备份全部同步至外部远程存储"
            >
              <Cloud className="h-4 w-4" />
              {syncingPool === 'all' ? '正在全量同步...' : '一键同步所有快照与备份'}
            </button>
          )}
          <button
            onClick={() => {
              setRemoteTestMessage(null)
              openAddRemoteModal()
            }}
            className="inline-flex items-center gap-1.5 rounded-md border border-blue-600 bg-blue-50 px-3 py-2 text-sm font-medium text-blue-700 hover:bg-blue-100"
          >
            <Plus className="h-4 w-4" />
            {t('添加远程存储')}
          </button>
          <button onClick={fetchData} className="inline-flex items-center gap-2 rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50">
            <RefreshCw className="h-4 w-4" />{t('刷新')}
          </button>
          <button onClick={() => save()} disabled={saving} className="inline-flex items-center gap-2 rounded-md bg-black px-3 py-2 text-sm text-white hover:bg-gray-800 disabled:opacity-50">
            <Save className="h-4 w-4" />{t(saving ? '保存中...' : '保存')}
          </button>
        </div>
      </div>

      {saveMessage && (
        <div
          role="status"
          aria-live="polite"
          className={`flex items-center gap-2 rounded-md border px-3 py-2 text-sm ${
            saveMessage.type === 'success'
              ? 'border-emerald-200 bg-emerald-50 text-emerald-800'
              : 'border-red-200 bg-red-50 text-red-700'
          }`}
        >
          {saveMessage.type === 'success'
            ? <CheckCircle2 className="h-4 w-4 shrink-0" />
            : <AlertCircle className="h-4 w-4 shrink-0" />}
          <span>{t(saveMessage.text)}</span>
        </div>
      )}

      {/* Remote Storage Pools */}
      {remotePools.length > 0 && (
        <div className="overflow-hidden rounded-lg border border-blue-200 bg-white shadow-sm">
            <div className="flex items-center justify-between border-b border-blue-100 bg-blue-50/70 px-4 py-3">
              <div className="flex items-center gap-2 font-semibold text-blue-900 text-sm">
                <Cloud className="h-4 w-4 text-blue-600" />
                <span>远程异地存储 (SFTP / WebDAV / MinIO S3 / OneDrive / Google Drive)</span>
              </div>
            </div>
          <div className="divide-y divide-gray-100">
            {remotePools.map((rp) => (
              <div key={rp.id} className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between hover:bg-gray-50">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="font-semibold text-gray-900 text-sm">{rp.name}</span>
                    <span className="rounded bg-blue-100 px-2 py-0.5 text-[11px] font-mono uppercase text-blue-800 font-semibold">{rp.type}</span>
                    {rp.enabled ? (
                      <span className="rounded bg-emerald-50 px-2 py-0.5 text-[11px] text-emerald-700 border border-emerald-200">已启用</span>
                    ) : (
                      <span className="rounded bg-gray-100 px-2 py-0.5 text-[11px] text-gray-500">已停用</span>
                    )}
                  </div>
                  <div className="mt-1 text-xs text-gray-500 flex flex-wrap gap-x-4 gap-y-1">
                    {rp.type === 'sftp' && <span>服务器: {rp.config?.host}:{rp.config?.port || '22'} · 用户: {rp.config?.user} · 路径: {rp.config?.base_path || '/'}</span>}
                    {rp.type === 'webdav' && <span>WebDAV 地址: {rp.config?.url} · 用户: {rp.config?.user || '-'}</span>}
                    {(rp.type === 'minio' || rp.type === 's3') && <span>Endpoint: {rp.config?.endpoint} · Bucket: {rp.config?.bucket}</span>}
                    {rp.type === 'onedrive' && <span>OneDrive · 目录: {rp.config?.root_path || '/clicd-backups'} · Tenant: {rp.config?.tenant || 'common'}</span>}
                    {rp.type === 'googledrive' && <span>Google Drive · 目录: {rp.config?.root_folder_name || 'CLICD-Backups'}</span>}
                  </div>
                  <div className="mt-2 flex items-center gap-3 text-xs text-gray-600">
                    <label className="inline-flex items-center gap-1.5 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={!!rp.sync_snapshots}
                        onChange={(e) => {
                          const updated = pools.map((p) => (p.id === rp.id ? { ...p, sync_snapshots: e.target.checked } : p))
                          setPools(updated)
                          save(updated)
                        }}
                        className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                      />
                      <span>自动同步快照 (Auto-sync Snapshots)</span>
                    </label>
                    <label className="inline-flex items-center gap-1.5 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={!!rp.sync_backups}
                        onChange={(e) => {
                          const updated = pools.map((p) => (p.id === rp.id ? { ...p, sync_backups: e.target.checked } : p))
                          setPools(updated)
                          save(updated)
                        }}
                        className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                      />
                      <span>自动同步备份 (Auto-sync Backups)</span>
                    </label>
                  </div>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  <button
                    onClick={() => openEditRemoteModal(rp)}
                    className="inline-flex items-center gap-1 rounded border border-gray-200 bg-white px-2.5 py-1.5 text-xs text-gray-700 hover:bg-gray-50"
                  >
                    <Pencil className="h-3.5 w-3.5" />
                    编辑
                  </button>
                  <button
                    onClick={async () => {
                      if (!(await dialog.confirm('同步至该存储', `确定将系统中现有的所有快照与全量备份同步至「${rp.name}」吗？`))) return
                      setSyncingPool(rp.id)
                      try {
                        const res = await syncAllToRemoteStorage(rp.id)
                        dialog.alert('同步完成', res.data.message || '已成功同步至该存储池。')
                        fetchData()
                      } catch (err: unknown) {
                        const error = err as { response?: { data?: { message?: string } } }
                        dialog.alert('同步失败', error.response?.data?.message || '同步失败，请检查存储连通性')
                      } finally {
                        setSyncingPool(null)
                      }
                    }}
                    disabled={!!syncingPool}
                    className="inline-flex items-center gap-1 rounded border border-blue-200 bg-blue-50 px-2.5 py-1.5 text-xs text-blue-700 hover:bg-blue-100 disabled:opacity-50"
                    title="立即将系统所有快照与备份同步到此存储池"
                  >
                    <Cloud className="h-3.5 w-3.5" />
                    {syncingPool === rp.id ? '同步中...' : '一键同步现有快照/备份'}
                  </button>
                  <button
                    onClick={() => handleDeletePool(rp.id)}
                    className="inline-flex items-center gap-1 rounded border border-red-200 bg-red-50 px-2.5 py-1.5 text-xs text-red-600 hover:bg-red-100"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    删除
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Local Mounted Storage Disks */}
      <div className="overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm">
        <div className="hidden border-b border-gray-200 bg-gray-50 px-4 py-3 text-xs font-semibold text-gray-500 2xl:grid 2xl:grid-cols-[minmax(170px,0.65fr)_minmax(320px,1.2fr)_minmax(480px,1.8fr)] 2xl:gap-4">
          <div>{t('磁盘')}</div>
          <div>{t('空间分布')}</div>
          <div>{t('用于存储')}</div>
        </div>

        {mountedDisks.length === 0 ? (
          <div className="px-4 py-8 text-center text-sm text-gray-500">{t('未检测到已挂载的磁盘')}</div>
        ) : (
          <div className="divide-y divide-gray-100">
            {mountedDisks.map((disk) => {
              const pool = pools.find((item) => poolForDisk(item, disk))
              const contentUsage = contentUsageMap(pool?.content_usage || disk.content_usage || [])
              const clicdUsed = pool?.clicd_used_bytes || disk.clicd_used_bytes || 0
              return (
                <section
                  key={`${disk.path}-${disk.mount_point}`}
                  className="grid min-w-0 grid-cols-1 gap-4 px-4 py-4 hover:bg-gray-50/70 2xl:grid-cols-[minmax(170px,0.65fr)_minmax(320px,1.2fr)_minmax(480px,1.8fr)]"
                >
                  <div className="min-w-0">
                    <div className="mb-2 text-xs font-medium text-gray-500 2xl:hidden">{t('磁盘')}</div>
                    <div className="flex min-w-0 items-start gap-3">
                      <div className="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-gray-100 text-gray-600">
                        <HardDrive className="h-5 w-5" />
                      </div>
                      <div className="min-w-0">
                        <div className="truncate font-mono text-xs font-medium text-gray-900" title={disk.path || disk.name}>{disk.path || disk.name}</div>
                        <div className="mt-1 truncate text-xs text-gray-500" title={disk.model || disk.fstype || disk.type || '-'}>{disk.model || disk.fstype || disk.type || '-'}</div>
                        <div className="mt-1 truncate font-mono text-xs text-gray-400" title={disk.mount_point}>{disk.mount_point}</div>
                      </div>
                    </div>
                  </div>
                  <div className="min-w-0">
                    <div className="mb-2 text-xs font-medium text-gray-500 2xl:hidden">{t('空间分布')}</div>
                    <DiskUsageBar disk={disk} contentUsage={contentUsage} clicdUsed={clicdUsed} />
                  </div>
                  <div className="min-w-0">
                    <div className="mb-2 text-xs font-medium text-gray-500 2xl:hidden">{t('用于存储')}</div>
                    <div className="grid min-w-0 grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
                      {contentOptions.map(([value, label]) => {
                        const checked = (pool?.content_types || []).includes(value)
                        const isDefault = (pool?.default_contents || []).includes(value)
                        return (
                          <div key={value} className={`min-w-0 rounded-md border px-2.5 py-2 ${checked ? 'border-gray-300 bg-white' : 'border-gray-200 bg-gray-50'}`}>
                            <label className="flex cursor-pointer items-center gap-2 text-xs text-gray-700">
                              <input className="shrink-0" type="checkbox" checked={checked} onChange={() => toggleContent(disk, value)} />
                              <span className="truncate" title={t(label)}>{t(label)}</span>
                            </label>
                            {checked && (
                              <div className="mt-1.5 flex items-center justify-between gap-2 border-t border-gray-100 pt-1.5">
                                <span className="truncate text-[11px] text-gray-500">{t('默认盘')}</span>
                                <button
                                  type="button"
                                  role="switch"
                                  aria-checked={isDefault}
                                  title={isDefault ? `${t('关闭')} ${t(label)} ${t('默认盘')}` : `${t('设为')} ${t(label)} ${t('默认盘')}`}
                                  onClick={() => toggleDefault(disk, value)}
                                  className={`relative inline-flex h-5 w-9 shrink-0 appearance-none items-center rounded-full border p-0 transition-colors focus:outline-none focus:ring-2 focus:ring-black focus:ring-offset-1 ${isDefault ? 'border-black bg-black' : 'border-gray-300 bg-gray-200'}`}
                                >
                                  <span className={`pointer-events-none absolute left-0.5 top-0.5 block h-4 w-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${isDefault ? 'translate-x-4' : 'translate-x-0'}`} />
                                </button>
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  </div>
                </section>
              )
            })}
          </div>
        )}
      </div>

      {/* Add Remote Storage Modal */}
      {showRemoteModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="w-full max-w-lg rounded-xl bg-white p-6 shadow-xl space-y-4 max-h-[90vh] overflow-y-auto">
            <h2 className="text-lg font-bold text-gray-900">{editingRemoteId ? '编辑远程存储' : '添加外部远程存储 (异地灾备)'}</h2>

            <div>
              <label className="block text-xs font-medium text-gray-700 mb-1">存储名称</label>
              <input
                type="text"
                value={remoteDraft.name}
                onChange={(e) => setRemoteDraft({ ...remoteDraft, name: e.target.value })}
                placeholder="例如: AWS-S3备份 / 异地SFTP主机"
                className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-black focus:outline-none"
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-gray-700 mb-1">协议类型</label>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
                {(['sftp', 'webdav', 'minio', 'onedrive', 'googledrive'] as const).map((t) => (
                  <button
                    key={t}
                    type="button"
                    onClick={() => switchRemoteType(t)}
                    className={`rounded-lg border px-3 py-2 text-xs font-semibold uppercase ${
                      remoteDraft.type === t ? 'border-blue-600 bg-blue-50 text-blue-700' : 'border-gray-200 text-gray-600 hover:bg-gray-50'
                    }`}
                  >
                    {t === 'minio' ? 'MinIO / S3' : t === 'googledrive' ? 'Google Drive' : t}
                  </button>
                ))}
              </div>
            </div>

            {remoteDraft.type === 'sftp' && (
              <div className="space-y-3 rounded-lg border border-gray-200 bg-gray-50/50 p-3 text-xs">
                <div className="grid grid-cols-3 gap-2">
                  <div className="col-span-2">
                    <label className="block text-gray-600 mb-1">主机 IP / 域名</label>
                    <input
                      value={remoteDraft.config.host}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, host: e.target.value } })}
                      placeholder="1.2.3.4"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                  <div>
                    <label className="block text-gray-600 mb-1">端口</label>
                    <input
                      value={remoteDraft.config.port}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, port: e.target.value } })}
                      placeholder="22"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label className="block text-gray-600 mb-1">用户名</label>
                    <input
                      value={remoteDraft.config.user}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, user: e.target.value } })}
                      placeholder="root"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                  <div>
                    <label className="block text-gray-600 mb-1">密码</label>
                    <input
                      type="password"
                      value={remoteDraft.config.password}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, password: e.target.value } })}
                      placeholder="密码"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                </div>
                <div>
                  <label className="block text-gray-600 mb-1">远端根目录路径</label>
                  <input
                    value={remoteDraft.config.base_path}
                    onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, base_path: e.target.value } })}
                    placeholder="/clicd-backups"
                    className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                  />
                </div>
              </div>
            )}

            {remoteDraft.type === 'webdav' && (
              <div className="space-y-3 rounded-lg border border-gray-200 bg-gray-50/50 p-3 text-xs">
                <div>
                  <label className="block text-gray-600 mb-1">WebDAV URL</label>
                  <input
                    value={remoteDraft.config.url}
                    onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, url: e.target.value } })}
                    placeholder="https://dav.jianguoyun.com/dav/ 或 http://nas:5005/dav"
                    className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                  />
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label className="block text-gray-600 mb-1">用户名</label>
                    <input
                      value={remoteDraft.config.user}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, user: e.target.value } })}
                      placeholder="username"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                  <div>
                    <label className="block text-gray-600 mb-1">密码 / 应用凭证</label>
                    <input
                      type="password"
                      value={remoteDraft.config.password}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, password: e.target.value } })}
                      placeholder="password"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                </div>
              </div>
            )}

            {remoteDraft.type === 'minio' && (
              <div className="space-y-3 rounded-lg border border-gray-200 bg-gray-50/50 p-3 text-xs">
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label className="block text-gray-600 mb-1">Endpoint 地址</label>
                    <input
                      value={remoteDraft.config.endpoint}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, endpoint: e.target.value } })}
                      placeholder="s3.amazonaws.com 或 1.2.3.4:9000"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                  <div>
                    <label className="block text-gray-600 mb-1">Bucket 名称</label>
                    <input
                      value={remoteDraft.config.bucket}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, bucket: e.target.value } })}
                      placeholder="clicd-backups"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label className="block text-gray-600 mb-1">Access Key</label>
                    <input
                      value={remoteDraft.config.access_key}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, access_key: e.target.value } })}
                      placeholder="AKIA..."
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                  <div>
                    <label className="block text-gray-600 mb-1">Secret Key</label>
                    <input
                      type="password"
                      value={remoteDraft.config.secret_key}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, secret_key: e.target.value } })}
                      placeholder="Secret Key"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                </div>
              </div>
            )}

            {remoteDraft.type === 'onedrive' && (
              <div className="space-y-3 rounded-lg border border-gray-200 bg-gray-50/50 p-3 text-xs">
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label className="block text-gray-600 mb-1">Client ID</label>
                    <input
                      value={remoteDraft.config.client_id}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, client_id: e.target.value } })}
                      placeholder="Azure 应用 client_id"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                  <div>
                    <label className="block text-gray-600 mb-1">Client Secret</label>
                    <input
                      type="password"
                      value={remoteDraft.config.client_secret}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, client_secret: e.target.value } })}
                      placeholder="Azure 应用 client_secret"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                </div>
                <div>
                  <label className="block text-gray-600 mb-1">Refresh Token</label>
                  <input
                    type="password"
                    value={remoteDraft.config.refresh_token}
                    onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, refresh_token: e.target.value } })}
                    placeholder="OAuth2 refresh_token"
                    className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white font-mono"
                  />
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label className="block text-gray-600 mb-1">远端目录</label>
                    <input
                      value={remoteDraft.config.root_path}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, root_path: e.target.value } })}
                      placeholder="/clicd-backups"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                  <div>
                    <label className="block text-gray-600 mb-1">Tenant（高级）</label>
                    <input
                      value={remoteDraft.config.tenant}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, tenant: e.target.value } })}
                      placeholder="common"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                </div>
                <p className="rounded bg-blue-50 px-2.5 py-2 text-[11px] text-blue-700 leading-relaxed">
                  💡 凭据获取：用 <code className="font-mono">rclone config</code> 配置一个 onedrive 远程（需勾选 Drive 权限），然后把 rclone.conf 里的 client_id、client_secret、refresh_token 抄到这里。Azure 应用需授予 Files.ReadWrite.All + offline_access 权限。
                </p>
              </div>
            )}

            {remoteDraft.type === 'googledrive' && (
              <div className="space-y-3 rounded-lg border border-gray-200 bg-gray-50/50 p-3 text-xs">
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label className="block text-gray-600 mb-1">Client ID</label>
                    <input
                      value={remoteDraft.config.client_id}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, client_id: e.target.value } })}
                      placeholder="Google OAuth client_id"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                  <div>
                    <label className="block text-gray-600 mb-1">Client Secret</label>
                    <input
                      type="password"
                      value={remoteDraft.config.client_secret}
                      onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, client_secret: e.target.value } })}
                      placeholder="Google OAuth client_secret"
                      className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                    />
                  </div>
                </div>
                <div>
                  <label className="block text-gray-600 mb-1">Refresh Token</label>
                  <input
                    type="password"
                    value={remoteDraft.config.refresh_token}
                    onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, refresh_token: e.target.value } })}
                    placeholder="OAuth2 refresh_token"
                    className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white font-mono"
                  />
                </div>
                <div>
                  <label className="block text-gray-600 mb-1">远端文件夹</label>
                  <input
                    value={remoteDraft.config.root_folder_name}
                    onChange={(e) => setRemoteDraft({ ...remoteDraft, config: { ...remoteDraft.config, root_folder_name: e.target.value } })}
                    placeholder="CLICD-Backups（不存在会自动创建；也可填 id:文件夹ID）"
                    className="w-full rounded border border-gray-300 px-2.5 py-1.5 bg-white"
                  />
                </div>
                <p className="rounded bg-blue-50 px-2.5 py-2 text-[11px] text-blue-700 leading-relaxed">
                  💡 凭据获取：用 <code className="font-mono">rclone config</code> 配置一个 drive 远程（scope 选 drive），然后把 rclone.conf 里的 client_id、client_secret、refresh_token 抄到这里。Google Cloud 项目需启用 Drive API。注意：OAuth 同意屏幕处于“测试”状态时 refresh_token 约 7 天过期。
                </p>
              </div>
            )}

            <div className="rounded-lg border border-blue-100 bg-blue-50/60 p-3 text-xs space-y-1.5">
              <div className="font-semibold text-blue-900">灾备自动同步选项：</div>
              <label className="flex items-center gap-2 text-gray-700 cursor-pointer">
                <input
                  type="checkbox"
                  checked={remoteDraft.sync_snapshots}
                  onChange={(e) => setRemoteDraft({ ...remoteDraft, sync_snapshots: e.target.checked })}
                  className="rounded border-gray-300 text-blue-600"
                />
                <span>创建快照时自动双写上传一份到此远程存储</span>
              </label>
              <label className="flex items-center gap-2 text-gray-700 cursor-pointer">
                <input
                  type="checkbox"
                  checked={remoteDraft.sync_backups}
                  onChange={(e) => setRemoteDraft({ ...remoteDraft, sync_backups: e.target.checked })}
                  className="rounded border-gray-300 text-blue-600"
                />
                <span>创建全量备份时自动双写上传一份到此远程存储</span>
              </label>
            </div>

            {remoteTestMessage && (
              <div className={`rounded p-2.5 text-xs font-medium ${remoteTestMessage.type === 'success' ? 'bg-emerald-50 text-emerald-800 border border-emerald-200' : 'bg-red-50 text-red-700 border border-red-200'}`}>
                {remoteTestMessage.text}
              </div>
            )}

            <div className="flex items-center justify-between pt-2 border-t">
              <button
                type="button"
                onClick={handleTestRemote}
                disabled={testingRemote}
                className="rounded border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-50"
              >
                {testingRemote ? '测试中...' : '测试连通性'}
              </button>
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => setShowRemoteModal(false)}
                  className="rounded border border-gray-300 px-3 py-1.5 text-xs text-gray-600 hover:bg-gray-50"
                >
                  取消
                </button>
                <button
                  type="button"
                  onClick={handleAddRemotePool}
                  className="rounded bg-black px-4 py-1.5 text-xs font-medium text-white hover:bg-gray-800"
                >
                  {editingRemoteId ? '保存修改' : '保存并启用'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function DiskUsageBar({
  disk,
  contentUsage,
  clicdUsed,
}: {
  disk: StorageDisk
  contentUsage: Record<string, number>
  clicdUsed: number
}) {
  const { t } = useLanguage()
  const total = Math.max(0, disk.size_bytes || 0)
  const free = Math.max(0, Math.min(total, disk.free_bytes || 0))
  const used = Math.max(0, total - free)
  const rawContentSegments = contentOptions.map(([value, label]) => ({
      key: value,
      label,
      size: Math.max(0, contentUsage[value] || 0),
      color: contentColors[value],
    }))
  const rawContentTotal = rawContentSegments.reduce((sum, segment) => sum + segment.size, 0)
  const normalizedClicdUsed = Math.max(0, Math.min(used, Math.max(clicdUsed || 0, rawContentTotal)))
  const contentScale = rawContentTotal > normalizedClicdUsed && rawContentTotal > 0
    ? normalizedClicdUsed / rawContentTotal
    : 1
  const contentSegments = rawContentSegments.map((segment) => ({ ...segment, size: segment.size * contentScale }))
  const categorizedClicdUsed = contentSegments.reduce((sum, segment) => sum + segment.size, 0)
  const unclassifiedClicdUsed = Math.max(0, normalizedClicdUsed - categorizedClicdUsed)
  const nonClicdUsed = Math.max(0, used - normalizedClicdUsed)
  const segments = [
    ...contentSegments,
    { key: 'clicd-other', label: 'CLICD 其他', size: unclassifiedClicdUsed, color: '#111827' },
    { key: 'other', label: '非 CLICD', size: nonClicdUsed, color: '#4b5563' },
    { key: 'free', label: '可用空间', size: free, color: '#e5e7eb' },
  ].filter((segment) => segment.size > 0)

  return (
    <div className="w-full min-w-0">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-xs text-gray-600">
        <span>{t('已用')} {formatBytes(used)} / {formatBytes(total)}</span>
        <span>{usagePct(used, total).toFixed(1)}% · {t('可用')} {formatBytes(free)}</span>
      </div>
      <div className="mt-2 flex h-8 w-full overflow-hidden rounded-md border border-gray-300 bg-gray-100">
        {segments.map((segment) => {
          const pct = usagePct(segment.size, total)
          return (
            <div
              key={segment.key}
              title={`${t(segment.label)}: ${formatBytes(segment.size)} (${pct.toFixed(2)}%)`}
              className="flex h-full items-center justify-center overflow-hidden border-r border-white/70 text-[10px] font-medium text-white last:border-r-0"
              style={{ width: `${pct}%`, minWidth: pct > 0 && pct < 0.6 ? '3px' : undefined, backgroundColor: segment.color }}
            >
              {pct >= 9 && <span className={segment.key === 'free' ? 'text-gray-600' : ''}>{t(segment.label)}</span>}
            </div>
          )
        })}
      </div>
      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1.5">
        {segments.map((segment) => (
          <div key={segment.key} className="flex items-center gap-1.5 text-[11px] text-gray-600">
            <span className="h-2.5 w-2.5 shrink-0 rounded-sm border border-black/5" style={{ backgroundColor: segment.color }} />
            <span>{t(segment.label)}</span>
            <span className="font-medium text-gray-800">{formatBytes(segment.size)}</span>
            <span className="text-gray-400">{usagePct(segment.size, total).toFixed(1)}%</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function poolForDisk(pool: StoragePool, disk: StorageDisk) {
  if (!disk.mount_point) return false
  const mount = cleanPath(disk.mount_point)
  const poolMount = cleanPath(pool.mount_point || '')
  const poolPath = cleanPath(pool.path || '')
  return poolMount === mount || poolPath === mount || poolPath.startsWith(`${mount}/`)
}

function defaultPoolForDisk(disk: StorageDisk): StoragePool {
  const mount = cleanPath(disk.mount_point || '/')
  const baseName = mount === '/' ? 'system' : mount.split('/').filter(Boolean).pop() || disk.name || 'disk'
  const primaryContents = mount === '/' ? contentOptions.map(([value]) => value) : []
  return {
    id: `disk-${slugID(mount === '/' ? 'root' : baseName)}`,
    name: `${baseName} (${disk.path || disk.name})`,
    path: mount === '/' ? '/var/lib/clicd' : `${mount}/clicd`,
    content_types: primaryContents,
    default_contents: [...primaryContents],
    enabled: true,
    mount_point: disk.mount_point,
  }
}

function cleanPath(value: string) {
  return value.replace(/\\/g, '/').replace(/\/+$/g, '') || '/'
}

function slugID(value: string) {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'storage'
}

function contentUsageMap(items: Array<{ content_type: string; size_bytes: number }>) {
  return items.reduce<Record<string, number>>((acc, item) => {
    acc[item.content_type] = (acc[item.content_type] || 0) + (item.size_bytes || 0)
    return acc
  }, {})
}

function usagePct(used: number, total: number) {
  if (!total || total <= 0) return 0
  return Math.max(0, Math.min(100, (used / total) * 100))
}

// defaultRemoteConfigFor presets every known config field for a protocol, so
// switching type in the add/edit modal does not leak fields across protocols.
function defaultRemoteConfigFor(type: 'sftp' | 'webdav' | 'minio' | 'onedrive' | 'googledrive'): Record<string, string> {
  return {
    host: '',
    port: '22',
    user: 'root',
    password: '',
    key: '',
    base_path: '/clicd-backups',
    url: '',
    endpoint: '',
    bucket: 'clicd',
    access_key: '',
    secret_key: '',
    region: 'us-east-1',
    use_ssl: 'true',
    client_id: '',
    client_secret: '',
    refresh_token: '',
    tenant: 'common',
    root_path: '/clicd-backups',
    root_folder_name: 'CLICD-Backups',
  }
}

function formatBytes(bytes: number) {
  if (!bytes) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index++
  }
  return `${value.toFixed(1)} ${units[index]}`
}
