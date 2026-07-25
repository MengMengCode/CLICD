import { useCallback, useEffect, useState } from 'react'
import { Camera, RefreshCw, Server, Trash2 } from 'lucide-react'
import { useNavigate } from 'react-router'
import { deleteContainerSnapshot, getSnapshots, Snapshot } from '../services/api'
import { useDialog } from '../components/Dialog'

export default function Snapshots() {
  const navigate = useNavigate()
  const dialog = useDialog()
  const [snapshots, setSnapshots] = useState<Snapshot[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [deleting, setDeleting] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    try {
      const res = await getSnapshots()
      setSnapshots(res.data.data || [])
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const handleDelete = async (snapshot: Snapshot) => {
    const confirmed = await dialog.confirm(
      '删除快照',
      `确认删除容器 ${snapshot.container_name} 的快照吗？此操作不可恢复。`
    )
    if (!confirmed) return

    setDeleting(snapshot.id)
    try {
      await deleteContainerSnapshot(snapshot.container_id, snapshot.id)
      setSnapshots(prev => prev.filter(s => s.id !== snapshot.id))
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      dialog.alert('删除失败', error.response?.data?.message || '请稍后重试')
    } finally {
      setDeleting(null)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-black" />
      </div>
    )
  }

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold text-black">快照管理</h1>
          <p className="mt-1 text-sm text-gray-500">全局快照列表，共 {snapshots.length} 个</p>
        </div>
        <button
          onClick={() => { setRefreshing(true); fetchData() }}
          disabled={refreshing}
          className="inline-flex items-center gap-2 rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50"
        >
          <RefreshCw className={`h-4 w-4 ${refreshing ? 'animate-spin' : ''}`} />
          刷新
        </button>
      </div>

      <div className="overflow-hidden rounded-lg border border-gray-200 bg-white">
        {snapshots.length === 0 ? (
          <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
            <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-lg bg-gray-100">
              <Camera className="h-7 w-7 text-gray-400" />
            </div>
            <div className="text-sm font-medium text-gray-700">暂无快照</div>
          </div>
        ) : (
          <table className="w-full min-w-[820px] text-sm">
            <thead className="border-b border-gray-200 bg-gray-50 text-xs text-gray-500">
              <tr>
                <th className="px-4 py-3 text-left font-medium">容器</th>
                <th className="px-4 py-3 text-left font-medium">LXC 名称</th>
                <th className="px-4 py-3 text-left font-medium">快照时间</th>
                <th className="px-4 py-3 text-left font-medium">类型</th>
                <th className="px-4 py-3 text-left font-medium">创建者</th>
                <th className="px-4 py-3 text-right font-medium">大小</th>
                <th className="px-4 py-3 text-center font-medium">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {snapshots.map((snapshot) => (
                <tr key={snapshot.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3">
                    <button
                      onClick={() => navigate(`/container/${snapshot.container_id}`)}
                      className="inline-flex items-center gap-2 text-left font-medium text-black hover:underline"
                    >
                      <Server className="h-4 w-4 text-gray-400" />
                      {snapshot.container_name}
                    </button>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-gray-600">{snapshot.lxc_name}</td>
                  <td className="px-4 py-3 text-gray-700">{snapshot.created_at}</td>
                  <td className="px-4 py-3">
                    <span className={`rounded px-2 py-1 text-xs ${snapshot.scheduled ? 'bg-blue-50 text-blue-700' : 'bg-gray-100 text-gray-700'}`}>
                      {snapshot.scheduled ? '定时' : '手动'}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-gray-600">{snapshot.created_by || '-'}</td>
                  <td className="px-4 py-3 text-right font-mono text-xs text-gray-600">{formatBytes(snapshot.size_bytes || 0)}</td>
                  <td className="px-4 py-3 text-center">
                    <button
                      onClick={() => handleDelete(snapshot)}
                      disabled={deleting === snapshot.id}
                      className="inline-flex items-center justify-center p-1.5 rounded text-red-500 hover:bg-red-50 transition-colors disabled:opacity-50"
                      title="删除快照"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

function formatBytes(bytes: number): string {
  if (!bytes) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`
}
