import { Button } from '../components/ui/button'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/table'
import { Card } from '../components/ui/card'
import { Badge } from '../components/ui/badge'
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
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
      </div>
    )
  }

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold text-foreground">快照管理</h1>
          <p className="mt-1 text-sm text-muted-foreground">全局快照列表，共 {snapshots.length} 个</p>
        </div>
        <Button variant="outline" size="sm"
          onClick={() => { setRefreshing(true); fetchData() }}
          disabled={refreshing}
          className="inline-flex items-center gap-2 px-3 py-2 text-sm"
        >
          <RefreshCw className={`h-4 w-4 ${refreshing ? 'animate-spin' : ''}`} />
          刷新
        </Button>
      </div>

      <Card className="overflow-hidden">
        {snapshots.length === 0 ? (
          <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
            <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-lg bg-muted">
              <Camera className="h-7 w-7 text-muted-foreground" />
            </div>
            <div className="text-sm font-medium text-foreground">暂无快照</div>
          </div>
        ) : (
          <Table className="w-full min-w-[820px] text-sm">
            <TableHeader className="text-xs">
              <TableRow>
                <TableHead className="px-4 py-3 text-left font-medium">容器</TableHead>
                <TableHead className="px-4 py-3 text-left font-medium">LXC 名称</TableHead>
                <TableHead className="px-4 py-3 text-left font-medium">快照时间</TableHead>
                <TableHead className="px-4 py-3 text-left font-medium">类型</TableHead>
                <TableHead className="px-4 py-3 text-left font-medium">创建者</TableHead>
                <TableHead className="px-4 py-3 text-right font-medium">大小</TableHead>
                <TableHead className="px-4 py-3 text-center font-medium">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody >
              {snapshots.map((snapshot) => (
                <TableRow key={snapshot.id} >
                  <TableCell className="px-4 py-3">
                    <Button variant="link" size="sm"
                      onClick={() => navigate(`/container/${snapshot.container_id}`)}
                      className="h-auto p-0 inline-flex items-center gap-2 text-left font-medium"
                    >
                      <Server className="h-4 w-4 text-muted-foreground" />
                      {snapshot.container_name}
                    </Button>
                  </TableCell>
                  <TableCell className="px-4 py-3 font-mono text-xs">{snapshot.lxc_name}</TableCell>
                  <TableCell className="px-4 py-3">{snapshot.created_at}</TableCell>
                  <TableCell className="px-4 py-3">
                    <Badge variant={snapshot.scheduled ? 'default' : 'secondary'}>
                      {snapshot.scheduled ? '定时' : '手动'}
                    </Badge>
                  </TableCell>
                  <TableCell className="px-4 py-3">{snapshot.created_by || '-'}</TableCell>
                  <TableCell className="px-4 py-3 text-right font-mono text-xs">{formatBytes(snapshot.size_bytes || 0)}</TableCell>
                  <TableCell className="px-4 py-3 text-center">
                    <Button variant="destructive" size="icon"
                      onClick={() => handleDelete(snapshot)}
                      disabled={deleting === snapshot.id}
                      className="h-8 w-8 inline-flex items-center justify-center"
                      title="删除快照"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
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
