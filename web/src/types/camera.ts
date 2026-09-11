export type CameraStatus = 'enabled' | 'disabled' | 'error';

export interface Camera {
  id: string;
  name: string;
  rtsp_uri: string;
  location?: string;
  fire_zone_id?: string;
  status: CameraStatus;
  config: Record<string, any>;
  created_at: string;
  updated_at: string;
}

export interface StreamInfo {
  camera_id: string;
  rtsp_url: string;
  hls_url: string;
  webrtc_url: string;
}