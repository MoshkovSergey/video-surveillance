import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getCameras } from '../api/client';
import MonitorStream from '../components/MonitorStream';
import type { Camera, CameraStatus } from '../types/camera';
import './MonitorPage.css';

const GRID_OPTIONS: readonly number[] = [6, 9, 12, 15, 18];

const GRID_COLS: Record<number, number> = {
  6: 3,
  9: 3,
  12: 4,
  15: 5,
  18: 6,
};

type SortMode = 'name' | 'status' | 'location';

const STATUS_RANK: Record<CameraStatus, number> = {
  enabled: 0,
  error: 1,
  disabled: 2,
};

const STORAGE_KEY = 'vs_monitor_state';

interface MonitorState {
  gridSize: number;
  layout: (string | null)[];
}

function loadState(): MonitorState | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as MonitorState) : null;
  } catch {
    return null;
  }
}

function normalizeLayout(layout: (string | null)[], size: number): (string | null)[] {
  const next = layout.slice(0, size);
  while (next.length < size) {
    next.push(null);
  }
  return next;
}

const initialState = loadState();
const initialSize =
  initialState && GRID_OPTIONS.includes(initialState.gridSize)
    ? initialState.gridSize
    : 9;

export default function MonitorPage() {
  const [gridSize, setGridSize] = useState<number>(initialSize);
  const [layout, setLayout] = useState<(string | null)[]>(() =>
    normalizeLayout(initialState?.layout ?? [], initialSize),
  );
  const [selectedCell, setSelectedCell] = useState<number | null>(null);
  const [sortMode, setSortMode] = useState<SortMode>('name');
  const [filter, setFilter] = useState('');

  // Раскладка монитора переживает перезагрузку страницы.
  useEffect(() => {
    const state: MonitorState = { gridSize, layout };
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  }, [gridSize, layout]);

  const { data: cameras } = useQuery({
    queryKey: ['cameras'],
    queryFn: getCameras,
    refetchInterval: 10_000,
  });

  const cameraById = useMemo(() => {
    const map = new Map<string, Camera>();
    for (const cam of cameras ?? []) {
      map.set(cam.id, cam);
    }
    return map;
  }, [cameras]);

  const sortedCameras = useMemo(() => {
    const list = [...(cameras ?? [])];
    const q = filter.trim().toLowerCase();

    const filtered = q
      ? list.filter(
          (c) =>
            c.name.toLowerCase().includes(q) ||
            (c.location ?? '').toLowerCase().includes(q),
        )
      : list;

    switch (sortMode) {
      case 'name':
        filtered.sort((a, b) => a.name.localeCompare(b.name, 'ru'));
        break;
      case 'status':
        filtered.sort(
          (a, b) =>
            STATUS_RANK[a.status] - STATUS_RANK[b.status] ||
            a.name.localeCompare(b.name, 'ru'),
        );
        break;
      case 'location':
        filtered.sort((a, b) =>
          (a.location ?? '\uffff').localeCompare(b.location ?? '\uffff', 'ru'),
        );
        break;
    }

    return filtered;
  }, [cameras, sortMode, filter]);

  const cols = GRID_COLS[gridSize] ?? 3;
  const rows = gridSize / cols;

  const changeGridSize = (size: number) => {
    setGridSize(size);
    setLayout((prev) => normalizeLayout(prev, size));
    setSelectedCell(null);
  };

  const clearCell = (index: number) => {
    setLayout((prev) => prev.map((value, i) => (i === index ? null : value)));
  };

  const clearAll = () => {
    setLayout(Array<string | null>(gridSize).fill(null));
    setSelectedCell(null);
  };

  const autofill = () => {
    setLayout(normalizeLayout(sortedCameras.map((c) => c.id), gridSize));
    setSelectedCell(null);
  };

  const assignCamera = (cameraId: string) => {
    setLayout((prev) => {
      const next = [...prev];
      const existing = next.indexOf(cameraId);

      let target =
        selectedCell !== null && selectedCell < next.length
          ? selectedCell
          : next.indexOf(null);

      if (target === -1) {
        // Свободных ячеек нет: без выбранной ячейки ничего не меняем.
        if (selectedCell === null) return prev;
        target = selectedCell;
      }

      if (existing !== -1) {
        // Камера уже на стене — меняем ячейки местами.
        next[existing] = next[target];
      }

      next[target] = cameraId;
      return next;
    });
    setSelectedCell(null);
  };

  return (
    <div className="monitor-page">
      <div className="monitor-main">
        <div className="monitor-toolbar">
          <div className="toolbar-group">
            <span className="toolbar-label">Сетка</span>
            {GRID_OPTIONS.map((n) => (
              <button
                key={n}
                className={`grid-btn ${n === gridSize ? 'active' : ''}`}
                onClick={() => changeGridSize(n)}
              >
                {n}
              </button>
            ))}
          </div>

          <div className="toolbar-group">
            <span className="toolbar-label">Сортировка</span>
            <select
              value={sortMode}
              onChange={(e) => setSortMode(e.target.value as SortMode)}
            >
              <option value="name">По имени</option>
              <option value="status">По статусу</option>
              <option value="location">По расположению</option>
            </select>
          </div>

          <div className="toolbar-group">
            <button className="btn-small" onClick={autofill}>
              Заполнить по порядку
            </button>
            <button className="btn-small btn-secondary" onClick={clearAll}>
              Очистить
            </button>
          </div>
        </div>

        <div
          className="monitor-grid"
          style={{ '--cols': cols, '--rows': rows } as CSSProperties}
        >
          {layout.map((camId, i) => {
            const cam = camId ? cameraById.get(camId) : undefined;

            return (
              <div
                key={i}
                className={[
                  'monitor-cell',
                  selectedCell === i ? 'cell-selected' : '',
                  cam ? '' : 'cell-empty',
                ].join(' ')}
                onClick={() => setSelectedCell(selectedCell === i ? null : i)}
              >
                {cam ? (
                  <>
                    <div className="cell-header">
                      <span className="cell-name">{cam.name}</span>
                      <span className={`status-dot status-${cam.status}`} />
                      <button
                        className="cell-clear"
                        title="Освободить ячейку"
                        onClick={(e) => {
                          e.stopPropagation();
                          clearCell(i);
                        }}
                      >
                        ×
                      </button>
                    </div>
                    <div className="cell-video">
                      <MonitorStream cameraId={cam.id} />
                    </div>
                  </>
                ) : (
                  <span className="cell-placeholder">{i + 1}</span>
                )}
              </div>
            );
          })}
        </div>
      </div>

      <aside className="monitor-side">
        <h2>Камеры</h2>

        <input
          className="side-filter"
          placeholder="Поиск по имени или месту"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />

        <div className="side-list">
          {sortedCameras.map((cam) => {
            const cellIdx = layout.indexOf(cam.id);
            return (
              <button
                key={cam.id}
                className="side-item"
                onClick={() => assignCamera(cam.id)}
                title="Назначить в выбранную ячейку"
              >
                <span className={`status-dot status-${cam.status}`} />
                <span className="side-item-name">{cam.name}</span>
                {cellIdx !== -1 && (
                  <span className="side-item-cell">{cellIdx + 1}</span>
                )}
              </button>
            );
          })}

          {sortedCameras.length === 0 && (
            <div className="side-empty">Ничего не найдено</div>
          )}
        </div>

        <p className="side-hint">
          Выберите ячейку сетки кликом, затем камеру из списка. Клик по камере
          без выбранной ячейки поместит её в первую свободную. Повторное
          назначение занятой камеры меняет ячейки местами.
        </p>
      </aside>
    </div>
  );
}