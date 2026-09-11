import type { Camera, StreamInfo } from '../types/camera';

const API_BASE = '/api/v1';

export async function getCameras(): Promise<Camera[]> {
  const res = await fetch(`${API_BASE}/cameras`);
  if (!res.ok) {
    throw new Error('Failed to fetch cameras');
  }
  return res.json();
}

export async function getCameraStream(cameraId: string): Promise<StreamInfo> {
  const res = await fetch(`${API_BASE}/cameras/${cameraId}/stream`);
  if (!res.ok) {
    throw new Error('Failed to fetch stream info');
  }
  return res.json();
}