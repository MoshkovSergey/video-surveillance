import type {
  Camera,
  CreateCameraPayload,
  StreamInfo,
  UpdateCameraPayload,
} from '../types/camera';
import type { Recording, RecordingsQuery } from '../types/recording';
import type { EventsQuery, SystemEvent } from '../types/event';
import type { AuthTokens, AuthUser, LoginPayload } from '../types/auth';

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

  // Защита от параллельных обновлений из нескольких запросов.
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

// ---------- Recordings ----------

export async function getRecordings(params: RecordingsQuery = {}): Promise<Recording[]> {
  const search = new URLSearchParams();
  if (params.camera_id) search.set('camera_id', params.camera_id);
  if (params.from) search.set('from', params.from);
  if (params.to) search.set('to', params.to);

  const qs = search.toString();
  const res = await apiFetch(`/recordings${qs ? `?${qs}` : ''}`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

// Файлы архива загружаются через авторизованный fetch,
// так как <video src> и <a href> не передают заголовок Authorization.
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