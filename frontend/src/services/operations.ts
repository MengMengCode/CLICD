import api, { type APIResponse, type ContainerIdentifier } from './api'

export interface DataDisk {
  id: string
  name: string
  size_gb: number
  allocated_bytes: number
  format: string
  target: string
  bus: string
  mount_path?: string
  storage_pool_id: string
  attached: boolean
  can_resize?: boolean
  error?: string
}
export interface DiskInventory {
  system: { id: string; size_gb: number; allocated_bytes: number; format: string; storage_pool_id: string; can_resize: boolean; error?: string }
  data_disks: DataDisk[]
  can_edit: boolean
  reason?: string
  limits: { max_data_disks: number }
}
export interface OperationAccepted { task_id: string; status?: string }
export interface PortableArtifact {
  id: string
  name: string
  kind: 'backup' | 'template'
  runtime: string
  arch: string
  created_at: string
  source_name: string
  size_bytes: number
  disk_count: number
  format_version: number
  credentials_preserved: boolean
  cold_copy: boolean
}
export interface RestoreInstanceInput {
  name: string
  storage_pool_id?: string
  assign_nat: boolean
  port_mapping_count: number
}
export const getDisks = (id: ContainerIdentifier) => api.get<APIResponse<DiskInventory>>(`/containers/${id}/disks`)
export const addDataDisk = (id: ContainerIdentifier, data: { name: string; size_gb: number; storage_pool_id?: string; mount_path?: string }) => api.post<APIResponse<OperationAccepted>>(`/containers/${id}/disks`, data)
export const resizeDisk = (id: ContainerIdentifier, diskId: string, size_gb: number) => api.put<APIResponse<OperationAccepted>>(`/containers/${id}/disks/${diskId}`, { size_gb })
export const attachDataDisk = (id: ContainerIdentifier, diskId: string, attached: boolean) => api.put<APIResponse<OperationAccepted>>(`/containers/${id}/disks/${diskId}/attachment`, { attached })
export const deleteDataDisk = (id: ContainerIdentifier, diskId: string) => api.delete<APIResponse<OperationAccepted>>(`/containers/${id}/disks/${diskId}`, { data: { delete_data: true } })
export const createBackup = (id: ContainerIdentifier, data: { name?: string; storage_pool_id?: string }) => api.post<APIResponse<OperationAccepted>>(`/containers/${id}/backup`, data)
export const cloneInstance = (id: ContainerIdentifier, data: RestoreInstanceInput) => api.post<APIResponse<OperationAccepted>>(`/containers/${id}/clone`, data)
export const saveInstanceTemplate = (id: ContainerIdentifier, data: { name: string; storage_pool_id?: string }) => api.post<APIResponse<OperationAccepted>>(`/containers/${id}/template`, data)
export const listBackups = () => api.get<APIResponse<PortableArtifact[]>>('/backups')
export const listInstanceTemplates = () => api.get<APIResponse<PortableArtifact[]>>('/instance-templates')
export const deleteArtifact = (kind: 'backup' | 'template', id: string) => api.delete<APIResponse>(`/${kind === 'backup' ? 'backups' : 'instance-templates'}/${id}`)
export const restoreArtifact = (kind: 'backup' | 'template', id: string, data: RestoreInstanceInput) => api.post<APIResponse<OperationAccepted>>(`/${kind === 'backup' ? 'backups' : 'instance-templates'}/${id}/${kind === 'backup' ? 'restore' : 'instantiate'}`, data)
export const backupDownloadTicket = (id: string) => api.post<APIResponse<{ url: string; expires_at: string }>>(`/backups/${id}/download-ticket`, {})
export const importBackup = (file: File, storagePool: string, onProgress: (value: number) => void, signal?: AbortSignal) => {
  const body = new FormData()
  body.append('file', file)
  if (storagePool) body.append('storage_pool_id', storagePool)
  return api.post<APIResponse<PortableArtifact>>('/backups', body, { headers: { 'Content-Type': undefined }, timeout: 0, params: { storage_pool_id: storagePool || undefined }, signal, onUploadProgress: e => onProgress(e.total ? Math.min(99, Math.round(e.loaded * 100 / e.total)) : 0) })
}
export const uploadLocalImage = (file: File, metadata: Record<string, string>, storagePool: string, onProgress: (value: number) => void, signal?: AbortSignal) => {
  const body = new FormData()
  Object.entries(metadata).forEach(([key, value]) => body.append(key, value))
  body.append('file', file)
  return api.post<APIResponse<{ id: string; name: string; format: string; size_bytes: number; sha256: string }>>('/images/upload', body, { headers: { 'Content-Type': undefined }, timeout: 0, params: { storage_pool_id: storagePool || undefined }, signal, onUploadProgress: e => onProgress(e.total ? Math.min(99, Math.round(e.loaded * 100 / e.total)) : 0) })
}
export function operationError(error: unknown) {
  return (error as { response?: { data?: { message?: string } }; message?: string })?.response?.data?.message || (error as Error)?.message || '操作失败'
}
export function formatBytes(bytes: number) {
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GiB`
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(1)} MiB`
  return `${Math.max(0, bytes)} B`
}
