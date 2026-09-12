import type {
  Camera,
  CreateCameraPayload,
  StreamInfo,
  UpdateCameraPayload,
  ONVIFParams,
  ONVIFProfile,
} from '../types/camera';
import type { Recording, RecordingsQuery } from '../types/recording';
import type { EventsQuery, SystemEvent } from '../types/event';
import type {
  AuthTokens,
  AuthUser,
  CreateUserPayload,
  LoginPayload,
  UpdateUserPayload,
  UserDTO,
} from '../types/auth';
import type { ScanResult } from '../types/discovery';
import type {
  SettingsDTO,
  TelegramTestPayload,
  UpdateSettingsPayload,
} from '../types/settings';

const API_BASE = '/api/v1';

const ACCESS_KEY = 'vs_access_token';
const REFRESH_KEY = 'vs_refresh_token';
const USER_KEY = 'vs_user';

// ---------- Хранилище сессии ----------

export function getStoredUser(): AuthUser | null {
  try {
    const raw = localStorage.getItem(USER_KEY);
    return raw ? (JSON.parse(raw) as AuthUser) : null;
  } catch {
    return null;
  }
}

export function clearAuthStorage(): void {
  localStorage.removeItem(ACCESS_KEY);
  localStorage.removeItem(REFRESH_KEY);
  localStorage.removeItem(USER_KEY);
}

function storeTokens(tokens: AuthTokens): void {
  localStorage.setItem(ACCESS_KEY, tokens.access_token);
  localStorage.setItem(REFRESH_KEY, tokens.refresh_token);
  localStorage.setItem(USER_KEY, JSON.stringify(tokens.user));
}

// ---------- Ошибки ----------

async function parseError(res: Response): Promise<string> {
  const body = await res.json().catch(() => null);
  return body?.error ?? `Запрос завершился с ошибкой (статус ${res.status})`;
}

// ---------- Обновление токена ----------

let refreshPromise: Promise<boolean> | null = null;

async function tryRefresh(): Promise<boolean> {
  const refresh = localStorage.getItem(REFRESH_KEY);
  if (!refresh) return false;

  if (!refreshPromise) {
    refreshPromise = (async () => {
      try {
        const res = await fetch(`${API_BASE}/auth/refresh`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ refresh_token: refresh }),
        });
        if (!res.ok) return false;
        const tokens = (await res.json()) as AuthTokens;
        storeTokens(tokens);
        return true;
      } catch {
        return false;
      } finally {
        setTimeout(() => {
          refreshPromise = null;
        }, 0);
      }
    })();
  }

  return refreshPromise;
}

// ---------- Центральный fetch с авторизацией ----------

async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const build = (): RequestInit => {
    const headers = new Headers(init.headers);
    const token = localStorage.getItem(ACCESS_KEY);
    if (token) {
      headers.set('Authorization', `Bearer ${token}`);
    }
    return { ...init, headers };
  };

  let res = await fetch(`${API_BASE}${path}`, build());

  if (res.status === 401 && !path.startsWith('/auth/')) {
    const refreshed = await tryRefresh();
    if (refreshed) {
      res = await fetch(`${API_BASE}${path}`, build());
    } else {
      clearAuthStorage();
      window.location.assign('/login');
      throw new Error('Сессия завершена, требуется вход');
    }
  }

  return res;
}

// ---------- Auth ----------

export async function login(payload: LoginPayload): Promise<AuthTokens> {
  const res = await fetch(`${API_BASE}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  const tokens = (await res.json()) as AuthTokens;
  storeTokens(tokens);
  return tokens;
}

// ---------- Users (admin only) ----------

export async function getUsers(): Promise<UserDTO[]> {
  const res = await apiFetch('/users');
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function createUser(payload: CreateUserPayload): Promise<UserDTO> {
  const res = await apiFetch('/users', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function updateUser(id: string, payload: UpdateUserPayload): Promise<UserDTO> {
  const res = await apiFetch(`/users/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function deleteUser(id: string): Promise<void> {
  const res = await apiFetch(`/users/${id}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
}

// ---------- Settings (admin only) ----------

export async function getSettings(): Promise<SettingsDTO> {
  const res = await apiFetch('/settings');
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function updateSettings(payload: UpdateSettingsPayload): Promise<SettingsDTO> {
  const res = await apiFetch('/settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function testTelegram(payload: TelegramTestPayload = {}): Promise<void> {
  const res = await apiFetch('/settings/telegram/test', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
}

// ---------- Discovery (admin only) ----------

export async function scanNetwork(): Promise<ScanResult> {
  const res = await apiFetch('/discovery/scan', { method: 'POST' });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return (await res.json()) as ScanResult;
}

// ---------- ONVIF ----------

export async function probeOnvif(params: ONVIFParams): Promise<ONVIFProfile[]> {
  const res = await apiFetch('/onvif/probe', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify(params),
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

// ---------- Cameras ----------

export async function getCameras(): Promise<Camera[]> {
  const res = await apiFetch('/cameras');
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function createCamera(payload: CreateCameraPayload): Promise<Camera> {
  const res = await apiFetch('/cameras', {
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
  const res = await apiFetch(`/cameras/${id}`, {
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
  const res = await apiFetch(`/cameras/${id}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
}

export async function getCameraStream(cameraId: string): Promise<StreamInfo> {
  const res = await apiFetch(`/cameras/${cameraId}/stream`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

// ---------- Live audio (транскодинг G.711 -> AAC) ----------

export async function startCameraAudio(cameraId: string): Promise<void> {
  const res = await apiFetch(`/cameras/${cameraId}/audio/start`, { method: 'POST' });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
}

export async function stopCameraAudio(cameraId: string): Promise<void> {
  const res = await apiFetch(`/cameras/${cameraId}/audio/stop`, { method: 'POST' });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
}

export async function getCameraAudioStatus(
  cameraId: string,
): Promise<{ running: boolean; available: boolean }> {
  const res = await apiFetch(`/cameras/${cameraId}/audio/status`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

// ---------- Recordings ----------

export async function getRecordings(params: RecordingsQuery = {}): Promise<Recording[]> {
  const search = new URLSearchParams();
  if (params.camera_id) search.set('camera_id', params.camera_id);
  if (params.from) search.set('from', params.from);
  if (params.to) search.set('to', params.to);
  if (params.kept === true) search.set('kept', 'true');
  if (params.kept === false) search.set('kept', 'false');

  const qs = search.toString();
  const res = await apiFetch(`/recordings${qs ? `?${qs}` : ''}`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function getRecordingBlob(recordingId: string): Promise<Blob> {
  const res = await apiFetch(`/recordings/${recordingId}/file`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.blob();
}

// ---------- Events ----------

export async function getEvents(params: EventsQuery = {}): Promise<SystemEvent[]> {
  const search = new URLSearchParams();
  if (params.camera_id) search.set('camera_id', params.camera_id);
  if (params.type) search.set('type', params.type);
  if (params.from) search.set('from', params.from);
  if (params.to) search.set('to', params.to);
  if (params.limit) search.set('limit', String(params.limit));

  const qs = search.toString();
  const res = await apiFetch(`/events${qs ? `?${qs}` : ''}`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}