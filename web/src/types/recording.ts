export interface Recording {
  id: string;
  camera_id: string;
  started_at: string;
  ended_at?: string;
  storage_path: string;
  size_bytes: number;
  kept: boolean;
  created_at: string;
}

export interface RecordingsQuery {
  camera_id?: string;
  from?: string;
  to?: string;
  kept?: boolean;
  limit?: number;
  offset?: number;
}