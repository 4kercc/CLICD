/**
 * Sanity check for the i18n pipeline: runs realistic runtime strings (with real
 * numbers and interpolated values) through the same translateText logic and
 * fails if any Chinese survives.
 *
 * Usage: node scripts/i18n-check.mjs
 */
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', 'src')
const src = readFileSync(join(ROOT, 'utils', 'i18n.ts'), 'utf8')

const exactBlock = src.slice(src.indexOf('const exact'), src.indexOf('const replacements'))
const exact = {}
for (const m of exactBlock.matchAll(/^\s*'((?:[^'\\]|\\.)*)':\s*(?:'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)")\s*,/gm)) {
  exact[m[1].replace(/\\'/g, "'")] = (m[2] !== undefined ? m[2] : m[3]).replace(/\\'/g, "'")
}
const replBlock = src.slice(src.indexOf('const replacements'), src.indexOf('export function translateText'))
const replacements = []
for (const m of replBlock.matchAll(/\[\s*\/((?:[^/\\\n]|\\.)+)\/([gimsuy]*)\s*,\s*'((?:[^'\\]|\\.)*)'\s*\]/g)) {
  replacements.push([new RegExp(m[1], m[2]), m[3].replace(/\\'/g, "'")])
}
const sortedExact = Object.entries(exact).sort((a, b) => b[0].length - a[0].length)
const CJK = /[\u3400-\u9fff]/

function translateText(value) {
  const body = value.trim()
  if (exact[body]) return exact[body]
  let out = body
  for (const [re, rep] of replacements) out = out.replace(re, rep)
  for (const [s, d] of sortedExact) out = out.split(s).join(d)
  return out.replace(/\s{2,}/g, ' ').trim()
}

// Real runtime shapes (numbers interpolated where the UI does that).
const samples = [
  '第 3 台容器',
  '第 12 台容器',
  '第 2 台容器的管理端口',
  '第 5 条映射展开后的公网端口超出 1-65535',
  '第 7 条映射的容器端口必须在 1-65535 之间',
  '批量端口冲突：22001/tcp 已被 第 3 台容器的管理端口 占用',
  '批量端口冲突：22001/tcp 已被 第 3 台容器 占用',
  '轻量级 COW 增量快照，共 12 个',
  '全量压缩独立归档备份，共 3 个',
  '确认彻底删除容器 jsq-win-2019 的全量备份文件吗？',
  '引导顺序',
  '引导/硬件',
  '未安装/未运行',
  '安装指南',
  'KVM物理占用',
  '安装Agent',
  '磁盘总线',
  '网卡驱动',
  '移除第三方镜像源和缓存',
  '每页数量',
  '子用户每台容器快照上限',
  '容器历史指标（后台每 30 秒采集）',
  '宿主机历史指标（后台每 30 秒采集）',
  '推送登录成功提醒',
  '管理员或子用户成功登录面板时，实时推送账号、来源 IP 与客户端信息（登录失败不推送）',
  '当前容器未分配 IPv4 NAT、独立公网 IPv4 或 IPv6，暂无可配置网络。',
  '外部磁盘镜像已成功导入并就绪。',
  '数据已成功从远程存储同步并还原。',
  '该实例已有任务排队或正在执行，请等待当前任务结束后重试。',
  '双因素认证 (2FA) 已成功启用！',
  '测试消息已成功发送至 Telegram，请打开 TG 查看',
  '仅控制子用户能看到/重装哪些系统，不影响上面选的安装系统。',
  '已成功将 5 个快照任务加入任务队列，系统将在后台自动排队执行',
  '存储位置：本地',
  '例如: socks5://127.0.0.1:10808 或 http://127.0.0.1:7890',
  'https://dav.jianguoyun.com/dav/ 或 http://nas:5005/dav',
  '确定移除该镜像源和已下载的缓存吗？正在使用该镜像的虚拟机不会允许移除。',
  'NAT4 范围必须是 1-65535，且起始端口不能大于结束端口',
  '支持管理宿主机本地挂载磁盘与远程对象存储/文件系统（SFTP / WebDAV / MinIO S3），支持快照/备份异地双写同步。',
]

let failures = 0
for (const sample of samples) {
  const out = translateText(sample)
  const ok = !CJK.test(out)
  if (!ok) failures++
  console.log(`${ok ? 'OK  ' : 'FAIL'}  ${sample}\n      -> ${out}`)
}
console.log(`\n${samples.length - failures}/${samples.length} translated cleanly`)
process.exit(failures === 0 ? 0 : 1)
