export type EventSeverity = 'info' | 'warning' | 'critical';

export type EventType =
  | 'motion'
  | 'camera_online'
  | 'camera_offline'
  | 'fire_alarm'
  | 'smoke_detection'
  | 'manual_alarm'
  | 'recording_error';

export interface SystemEvent {
  id: string;
  camera_id?: string;
  type: EventType;
  severity: EventSeverity;
  occurred_at: string;
  payload: Record<string, unknown>;
  created_at: string;
}

export interface EventsQuery {
  camera_id?: string;
  type?: string;
  from?: string;
  to?: string;
  limit?: number;
}