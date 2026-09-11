export type CameraStatus = 'enabled' | 'disabled' | 'error';

export type CameraSourceType = 'rtsp' | 'onvif';

// ONVIFParams — параметры подключения к ONVIF-устройству.
export interface ONVIFParams {
  host: string;
  port?: number;
  username?: string;
  password?: string;
  profile?: string;
}

export interface Camera {
  id: string;
  name: string;
  rtsp_uri: string;
  location?: string;
  fire_zone_id?: string;
  status: CameraStatus;
  source_type: CameraSourceType;
  onvif?: ONVIFParams;
  config?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface StreamInfo {
  camera_id: string;
  rtsp_url: string;
  hls_url: string;
  webrtc_url: string;
}

export interface CreateCameraPayload {
  name: string;
  rtsp_uri?: string;
  location?: string;
  fire_zone_id?: string;
  source_type?: CameraSourceType;
  onvif?: ONVIFParams;
  config?: Record<string, unknown>;
}

export interface UpdateCameraPayload {
  name?: string;
  rtsp_uri?: string;
  location?: string;
  fire_zone_id?: string;
  status?: CameraStatus;
  source_type?: CameraSourceType;
  onvif?: ONVIFParams;
  config?: Record<string, unknown>;
}

export interface ONVIFProfile {
  token: string;
  name: string;
  stream_uri: string;
}