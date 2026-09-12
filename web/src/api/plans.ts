import type { PlanDTO, PlanObjectInput, PlanStatus } from '../types/plan';
import { apiFetch } from './client';

async function parseError(res: Response): Promise<string> {
  const body = await res.json().catch(() => null);
  return body?.error ?? `Запрос завершился с ошибкой (статус ${res.status})`;
}

export async function getPlans(): Promise<PlanDTO[]> {
  const res = await apiFetch('/plans');
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function getPlan(id: string): Promise<PlanDTO> {
  const res = await apiFetch(`/plans/${id}`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function createPlan(name: string, image: File): Promise<PlanDTO> {
  const form = new FormData();
  form.append('name', name);
  form.append('image', image);

  const res = await apiFetch('/plans', { method: 'POST', body: form });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function renamePlan(id: string, name: string): Promise<void> {
  const res = await apiFetch(`/plans/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
}

export async function deletePlan(id: string): Promise<void> {
  const res = await apiFetch(`/plans/${id}`, { method: 'DELETE' });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
}

export async function putPlanObjects(id: string, objects: PlanObjectInput[]): Promise<void> {
  const res = await apiFetch(`/plans/${id}/objects`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify({ objects }),
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
}

export async function getPlanStatus(id: string): Promise<PlanStatus> {
  const res = await apiFetch(`/plans/${id}/status`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.json();
}

export async function getPlanImageBlob(id: string): Promise<Blob> {
  const res = await apiFetch(`/plans/${id}/image`);
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res.blob();
}