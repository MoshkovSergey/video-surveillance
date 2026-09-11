import type {
  Camera,
  CreateCameraPayload,
  StreamInfo,
  UpdateCameraPayload,
} from '../types/camera';
import type { Recording, RecordingsQuery } from '../types/recording';

const API_BASE = '/api/v1';

// Извлекает текст ошибки из ответа бэкенда
async function parseError(res: Response): Promise<string> {
  const body = await res.json().catch(() => null);
  return body?.error ?? `Запрос завершился с ошибкой (статус ${res.status})`;
}

export async function getCameras(): Promise<Camera[]> {
  const res = await fetch(`${API_BASE}/cameras`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function createCamera(payload: CreateCameraPayload): Promise<Camera> {
  const res = await fetch(`${API_BASE}/cameras`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function updateCamera(id: string, payload: UpdateCameraPayload): Promise<Camera> {
  const res = await fetch(`${API_BASE}/cameras/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function deleteCamera(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/cameras/${id}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
}

export async function getCameraStream(cameraId: string): Promise<StreamInfo> {
  const res = await fetch(`${API_BASE}/cameras/${cameraId}/stream`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function getRecordings(params: RecordingsQuery = {}): Promise<Recording[]> {
  const search = new URLSearchParams();
  if (params.camera_id) search.set('camera_id', params.camera_id);
  if (params.from) search.set('from', params.from);
  if (params.to) search.set('to', params.to);

  const qs = search.toString();
  const res = await fetch(`${API_BASE}/recordings${qs ? `?${qs}` : ''}`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export function recordingFileUrl(recordingId: string): string {
  return `${API_BASE}/recordings/${recordingId}/file`;
}