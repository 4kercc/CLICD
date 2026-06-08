import { useCallback, useEffect, useState } from 'react'
import { Clock, Globe, LogIn, Monitor, UserCog } from 'lucide-react'
import {
  changePassword,
  changeUsername,
  getLoginLogs,
  LoginLog,
} from '../services/api'
import { useDialog } from '../components/Dialog'
import { useAuth } from '../contexts/AuthContext'

export default function Settings() {
  const dialog = useDialog()
  const { username } = useAuth()
  const [logs, setLogs] = useState<LoginLog[]>([])
  const [loading, setLoading] = useState(true)
  const [logPage, setLogPage] = useState(1)
  const pageSize = 10

  const [oldPwd, setOldPwd] = useState('')
  const [newPwd, setNewPwd] = useState('')
  const [newUsername, setNewUsername] = useState('')

  const fetchLogs = useCallback(async () => {
    try {
      const res = await getLoginLogs()
      if (res.data.data) setLogs(res.data.data)
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchLogs()
    const timer = setInterval(fetchLogs, 15000)
    return () => clearInterval(timer)
  }, [fetchLogs])

  const handleSaveAccount = async () => {
    if (!oldPwd) {
      dialog.alert('提示', '请输入当前密码以确认修改')
      return
    }
    if (!newPwd && !newUsername) {
      dialog.alert('提示', '至少填写新密码或新用户名中的一项')
      return
    }
    if (newPwd && newPwd.length < 6) {
      dialog.alert('提示', '新密码至少 6 位')
      return
    }
    if (newUsername && newUsername.length < 3) {
      dialog.alert('提示', '用户名至少 3 位')
      return
    }

    const results: string[] = []
    try {
      if (newUsername) {
        const res = await changeUsername(newUsername, oldPwd)
        results.push(res.data.success ? '用户名已修改' : '用户名修改失败')
      }
      if (newPwd) {
        const res = await changePassword(oldPwd, newPwd)
        results.push(res.data.success ? '密码已修改' : '密码修改失败')
      }
      if (results.length > 0) {
        dialog.alert('完成', `${results.join('，')}。下次登录生效`)
        setOldPwd('')
        setNewPwd('')
        setNewUsername('')
      }
    } catch (err: unknown) {
      const e = err as { response?: { data?: { message?: string } } }
      dialog.alert('失败', e.response?.data?.message || '修改失败')
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-black"></div>
      </div>
    )
  }

  const totalPages = Math.ceil(logs.length / pageSize)

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-black">面板设置</h1>
        <p className="mt-1 text-sm text-gray-500">账号管理与登录日志</p>
      </div>

      <div className="rounded-lg border border-gray-200 bg-white p-5">
        <h2 className="mb-4 flex items-center gap-2 text-sm font-semibold text-black">
          <UserCog className="h-4 w-4" />账号设置
        </h2>
        <div className="space-y-4">
          <div>
            <label className="mb-1 block text-xs text-gray-500">当前用户名</label>
            <input type="text" value={username || ''} disabled className="w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-400" />
          </div>
          <div>
            <label className="mb-1 block text-xs text-gray-500">新用户名（留空则不修改）</label>
            <input type="text" value={newUsername} onChange={(e) => setNewUsername(e.target.value)} className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-black" placeholder="至少 3 位" />
          </div>
          <div className="border-t border-gray-100 pt-3">
            <label className="mb-1 block text-xs text-gray-500">新密码（留空则不修改）</label>
            <input type="password" value={newPwd} onChange={(e) => setNewPwd(e.target.value)} className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-black" placeholder="至少 6 位" />
          </div>
          <div>
            <label className="mb-1 block text-xs text-gray-500">当前密码（验证身份）</label>
            <input type="password" value={oldPwd} onChange={(e) => setOldPwd(e.target.value)} className="w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm text-black" placeholder="输入当前密码以确认修改" />
          </div>
          <button onClick={handleSaveAccount} className="w-full rounded-md bg-black px-4 py-2 text-sm text-white hover:bg-gray-800">保存修改</button>
        </div>
      </div>

      <div className="rounded-lg border border-gray-200 bg-white p-5">
        <h2 className="mb-4 flex items-center gap-2 text-sm font-semibold text-black">
          <LogIn className="h-4 w-4" />登录日志
        </h2>
        {logs.length === 0 ? (
          <p className="text-sm text-gray-400">暂无登录记录</p>
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-gray-100 text-gray-400">
                    <th className="w-40 py-2 text-left font-medium"><span className="inline-flex items-center gap-1"><Clock className="h-3 w-3" />时间</span></th>
                    <th className="py-2 text-left font-medium">用户名</th>
                    <th className="py-2 text-left font-medium"><span className="inline-flex items-center gap-1"><Globe className="h-3 w-3" />IP</span></th>
                    <th className="py-2 text-left font-medium"><span className="inline-flex items-center gap-1"><Monitor className="h-3 w-3" />设备</span></th>
                    <th className="py-2 text-left font-medium">结果</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-50">
                  {logs.slice((logPage - 1) * pageSize, logPage * pageSize).map((log, index) => (
                    <tr key={`${log.time}-${index}`}>
                      <td className="whitespace-nowrap py-1.5 font-mono text-gray-500">{log.time}</td>
                      <td className="py-1.5 text-gray-700">{log.username}</td>
                      <td className="py-1.5 font-mono text-gray-500">{log.ip}</td>
                      <td className="max-w-[180px] truncate py-1.5 text-gray-500" title={log.user_agent}>{formatUA(log.user_agent)}</td>
                      <td className="py-1.5">
                        <span className={`rounded px-1.5 py-0.5 text-xs ${log.success ? 'bg-gray-100 text-gray-700' : 'bg-red-50 text-red-600'}`}>
                          {log.success ? '成功' : '失败'}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {logs.length > pageSize && (
              <div className="mt-3 flex items-center justify-between border-t border-gray-100 pt-3">
                <span className="text-xs text-gray-400">共 {logs.length} 条，第 {logPage}/{totalPages} 页</span>
                <div className="flex items-center gap-1">
                  <button onClick={() => setLogPage(1)} disabled={logPage === 1} className="rounded border border-gray-200 px-2 py-1 text-xs hover:bg-gray-50 disabled:opacity-30">首页</button>
                  <button onClick={() => setLogPage(p => Math.max(1, p - 1))} disabled={logPage === 1} className="rounded border border-gray-200 px-2 py-1 text-xs hover:bg-gray-50 disabled:opacity-30">上一页</button>
                  {Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
                    let start = Math.max(1, logPage - 2)
                    if (start + 4 > totalPages) start = Math.max(1, totalPages - 4)
                    const page = start + i
                    if (page > totalPages) return null
                    return (
                      <button key={page} onClick={() => setLogPage(page)} className={`h-7 w-7 rounded text-xs ${page === logPage ? 'bg-black text-white' : 'border border-gray-200 hover:bg-gray-50'}`}>{page}</button>
                    )
                  })}
                  <button onClick={() => setLogPage(p => Math.min(totalPages, p + 1))} disabled={logPage >= totalPages} className="rounded border border-gray-200 px-2 py-1 text-xs hover:bg-gray-50 disabled:opacity-30">下一页</button>
                  <button onClick={() => setLogPage(totalPages)} disabled={logPage >= totalPages} className="rounded border border-gray-200 px-2 py-1 text-xs hover:bg-gray-50 disabled:opacity-30">末页</button>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function formatUA(ua: string): string {
  const parts: string[] = []
  if (ua.includes('Windows NT')) parts.push('Windows')
  else if (ua.includes('Mac OS X')) parts.push('macOS')
  else if (ua.includes('Linux')) parts.push('Linux')
  else if (ua.includes('Android')) parts.push('Android')
  else if (ua.includes('iPhone') || ua.includes('iPad')) parts.push('iOS')

  if (ua.includes('Chrome') && !ua.includes('Edg')) parts.push('Chrome')
  else if (ua.includes('Firefox')) parts.push('Firefox')
  else if (ua.includes('Edg')) parts.push('Edge')
  else if (ua.includes('Safari') && !ua.includes('Chrome')) parts.push('Safari')

  return parts.join(' / ') || ua.substring(0, 40)
}
