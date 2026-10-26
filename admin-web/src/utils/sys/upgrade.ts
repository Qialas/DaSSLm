import { StorageConfig } from '@/utils/storage/storage-config'
// Framework preferences remain versioned; no demo upgrade notifications or forced logout.
export function systemUpgrade(): void {
  localStorage.setItem(StorageConfig.VERSION_KEY, StorageConfig.CURRENT_VERSION)
}
