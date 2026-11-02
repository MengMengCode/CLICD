import { Button } from '../components/ui/button'
import { Card } from '../components/ui/card'
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from '../components/ui/table'
import { Badge } from '../components/ui/badge'
import { useCallback, useEffect, useState } from 'react'
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight, RefreshCw } from 'lucide-react'
import { AuditLog, getAuditLogs } from '../services/api'
import { actionLabel } from '../utils/labels'

const PAGE_SIZE = 10

export default function AuditLogs() {
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(1)

  const fetchData = useCallback(async () => {
    try {
      const res = await getAuditLogs()
      setLogs(res.data.data || [])
    } catch (err) {
      console.error(err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchData()
    const timer = window.setInterval(fetchData, 10000)
    return () => window.clearInterval(timer)
  }, [fetchData])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
      </div>
    )
  }

  const totalPages = Math.max(1, Math.ceil(logs.length / PAGE_SIZE))
  const pageLogs = logs.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold text-foreground">操作日志</h1>
          <p className="text-sm text-muted-foreground mt-1">共 {logs.length} 条操作记录</p>
        </div>
        <Button variant="outline"
          onClick={fetchData}
          className="h-auto inline-flex items-center gap-2 px-3 py-2 text-sm"
        >
          <RefreshCw className="w-4 h-4" />
          刷新
        </Button>
      </div>

      <Card className="overflow-hidden">
        {logs.length === 0 ? (
          <div className="p-8 text-center text-sm text-muted-foreground">暂无操作日志</div>
        ) : (
          <>
            <div className="overflow-x-auto">
              <Table className="w-full text-sm">
                <TableHeader>
                  <TableRow className="border-b border-border bg-muted text-left text-xs font-medium text-muted-foreground">
                    <TableHead className="h-auto px-4 py-2.5 whitespace-nowrap">时间</TableHead>
                    <TableHead className="h-auto px-4 py-2.5 whitespace-nowrap">用户</TableHead>
                    <TableHead className="h-auto px-4 py-2.5 whitespace-nowrap">操作</TableHead>
                    <TableHead className="h-auto px-4 py-2.5 whitespace-nowrap">目标</TableHead>
                    <TableHead className="h-auto px-4 py-2.5">详情</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody className="divide-y divide-border">
                  {pageLogs.map((log, index) => (
                    <TableRow key={`${log.time}-${index}`} className="hover:bg-muted/50">
                      <TableCell className="px-4 py-2.5 font-mono text-xs text-muted-foreground whitespace-nowrap">{log.time}</TableCell>
                      <TableCell className="px-4 py-2.5 whitespace-nowrap">
                        {log.user === 'admin' ? (
                          <Badge variant="default" className="inline-flex items-center gap-1 px-1.5 py-0.5 text-[11px] font-medium">管理员</Badge>
                        ) : log.user?.startsWith('user:') ? (
                          <Badge variant="secondary" className="inline-flex items-center gap-1 px-1.5 py-0.5 text-[11px] font-medium">用户</Badge>
                        ) : (
                          <span className="text-xs text-muted-foreground">{log.user || '-'}</span>
                        )}
                      </TableCell>
                      <TableCell className="px-4 py-2.5 text-foreground whitespace-nowrap">{actionLabel(log.action)}</TableCell>
                      <TableCell className="px-4 py-2.5 font-mono text-xs text-foreground whitespace-nowrap">{log.target || '-'}</TableCell>
                      <TableCell className="px-4 py-2.5 text-muted-foreground min-w-[280px]">{log.detail || '-'}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            {logs.length > PAGE_SIZE && (
              <div className="flex items-center justify-between px-4 py-3 border-t border-border bg-muted">
                <span className="text-xs text-muted-foreground">第 {page}/{totalPages} 页</span>
                <div className="flex items-center gap-1">
                  <Button variant="ghost" onClick={() => setPage(1)} disabled={page === 1} className="h-auto p-1" title="首页"><ChevronsLeft className="w-4 h-4" /></Button>
                  <Button variant="ghost" onClick={() => setPage(p => Math.max(1, p - 1))} disabled={page === 1} className="h-auto p-1" title="上一页"><ChevronLeft className="w-4 h-4" /></Button>
                  {getPageNumbers(page, totalPages).map(n => (
                    <Button variant={n === page ? 'default' : 'outline'} key={n} onClick={() => setPage(n)} className="w-7 h-7 p-0 text-xs" aria-current={n === page ? 'page' : undefined}>{n}</Button>
                  ))}
                  <Button variant="ghost" onClick={() => setPage(p => Math.min(totalPages, p + 1))} disabled={page >= totalPages} className="h-auto p-1" title="下一页"><ChevronRight className="w-4 h-4" /></Button>
                  <Button variant="ghost" onClick={() => setPage(totalPages)} disabled={page >= totalPages} className="h-auto p-1" title="末页"><ChevronsRight className="w-4 h-4" /></Button>
                </div>
              </div>
            )}
          </>
        )}
      </Card>
    </div>
  )
}

function getPageNumbers(current: number, total: number): number[] {
  if (total <= 5) return Array.from({ length: total }, (_, i) => i + 1)
  let start = Math.max(1, current - 2)
  if (start + 4 > total) start = total - 4
  return Array.from({ length: 5 }, (_, i) => start + i)
}
