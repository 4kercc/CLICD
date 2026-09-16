import { useEffect, useState } from 'react'
import { Camera, Server, X } from 'lucide-react'
import { useDialog } from './Dialog'
import {
  Container,
  getStorageInfo,
  StoragePool,
  batchAction,
} from '../services/api'

interface BatchSnapshotModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess: () => void
  selectedContainers: Container[]
}

export default function BatchSnapshotModal({
  isOpen,
  onClose,
  onSuccess,
  selectedContainers,
}: BatchSnapshotModalProps) {
  const dialog = useDialog()
  const [submitting, setSubmitting] = useState(false)
  const [storagePools, setStoragePools] = useState<StoragePool[]>([])
  const [selectedPoolID, setSelectedPoolID] = useState<string>('')

  useEffect(() => {
    if (isOpen) {
      getStorageInfo()
        .then((res) => {
          const pools = res.data.data?.pools || []
          setStoragePools(pools)
        })
        .catch(() => {})
    }
  }, [isOpen])

  if (!isOpen) return null

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitting(true)
    try {
      const containerIDs = selectedContainers.map((c) => c.id)
      await batchAction('snapshot', containerIDs, undefined, selectedPoolID || undefined)
      dialog.alert('已加入任务队列', `已成功将 ${selectedContainers.length} 个快照任务加入任务队列，系统将在后台自动排队执行`)
      onSuccess()
      onClose()
    } catch (err: any) {
      dialog.alert('提交失败', err?.response?.data?.message || '无法提交批量快照任务')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="flex max-h-[85vh] w-full max-w-lg flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-gray-200 px-6 py-4">
          <div className="flex items-center gap-2">
            <Camera className="h-5 w-5 text-black" />
            <div>
              <h2 className="text-base font-semibold text-black">批量创建快照</h2>
              <p className="text-xs text-gray-500">已选中 {selectedContainers.length} 台容器/虚拟机</p>
            </div>
          </div>
          <button onClick={onClose} className="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-black">
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Content */}
        <form onSubmit={handleSubmit} className="overflow-y-auto p-6 space-y-4">
          <div className="space-y-2">
            <label className="text-xs font-medium text-gray-700">目标容器列表：</label>
            <div className="max-h-44 overflow-y-auto rounded-md border border-gray-200 bg-gray-50 p-2 space-y-1">
              {selectedContainers.map((c) => (
                <div key={c.id} className="flex items-center justify-between bg-white px-2.5 py-1.5 rounded border border-gray-100 text-xs">
                  <div className="flex items-center gap-1.5 font-medium text-black">
                    <Server className="h-3.5 w-3.5 text-gray-400" />
                    {c.name}
                  </div>
                  <span className="text-[11px] text-gray-500">
                    配额: {c.snapshot_limit || 1} 个
                  </span>
                </div>
              ))}
            </div>
          </div>

          {storagePools.length > 0 && (
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-gray-700">存储位置：</label>
              <select
                value={selectedPoolID}
                onChange={(e) => setSelectedPoolID(e.target.value)}
                className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-xs text-black"
              >
                <option value="">默认存储池</option>
                {storagePools.map((pool) => (
                  <option key={pool.id} value={pool.id}>
                    {pool.name} ({(pool.type || 'local').toUpperCase()})
                  </option>
                ))}
              </select>
            </div>
          )}

          <div className="rounded-md bg-blue-50 p-3 text-xs text-blue-800 leading-relaxed">
            💡 快照将在后台任务队列中按并发限制排队执行，采用写时复制 (COW) 增量秒级生成；若已配置远程存储且开启快照同步，完成后将自动异步上传副本。
          </div>

          {/* Footer */}
          <div className="flex items-center justify-end gap-2 pt-3 border-t border-gray-200">
            <button
              type="button"
              onClick={onClose}
              disabled={submitting}
              className="rounded-md border border-gray-300 px-4 py-2 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={submitting}
              className="inline-flex items-center gap-1.5 rounded-md bg-black px-4 py-2 text-xs font-medium text-white hover:bg-gray-800 disabled:opacity-50"
            >
              {submitting ? '正在提交任务...' : '立即创建快照'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
