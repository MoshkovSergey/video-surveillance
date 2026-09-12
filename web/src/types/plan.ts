export type PlanObjectKind = 'camera' | 'exit' | 'zone';

export interface PlanObjectDTO {
  id: string;
  kind: PlanObjectKind;
  camera_id: string | null;
  camera_name?: string;
  label: string;
  x: number; // проценты от ширины схемы, 0..100
  y: number; // проценты от высоты схемы, 0..100
}

export interface PlanDTO {
  id: string;
  name: string;
  image_url: string;
  created_at: string;
  objects?: PlanObjectDTO[];
}

export interface PlanObjectInput {
  id?: string;
  kind: PlanObjectKind;
  camera_id?: string | null;
  label?: string;
  x: number;
  y: number;
}

export interface PlanStatus {
  cameras: Record<string, boolean>; // camera_id -> online
}