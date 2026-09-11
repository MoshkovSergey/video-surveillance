import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getCameras, getRecordings, recordingFileUrl } from '../api/client';
import { useToast } from '../components/Toast';
import type { Recording } from '../types/recording';
import './ArchivePage.css';

function formatBytes(size: number): string {
  if (size >= 1024 ** 3) return `${(size / 1024 ** 3).toFixed(1)} ГБ`;
  if (size >= 1024 ** 2) return `${(size / 1024 ** 2).toFixed(1)} МБ`;
  if (size >= 1024) return `${(size / 1024).toFixed(1)} КБ`;
  return `${size} Б`;
}

function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString();
}

function formatDuration(rec: Recording): string {
  if (!rec.ended_at) return 'запись идёт';
  const sec = Math.max(
    0,
    Math.round((new Date(rec.ended_at).getTime() - new Date(rec.started_at).getTime()) / 1000),
  );
  const min = Math.floor(sec / 60);
  const s = sec % 60;
  return `${min} мин ${s} с`;
}

function todayLocal(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export default function ArchivePage() {
  const notify = useToast();
  const [cameraId, setCameraId] = useState('');
  const [date, setDate] = useState(todayLocal);
  const [selected, setSelected] = useState<Recording | null>(null);

  const { data: cameras } = useQuery({
    queryKey: ['cameras'],
    queryFn: getCameras,
  });

  const { from, to } = useMemo(() => {
    if (!date) return { from: undefined, to: undefined };
    return {
      from: new Date(`${date}T00:00:00`).toISOString(),
      to: new Date(`${date}T23:59:59`).toISOString(),
    };
  }, [date]);

  const {
    data: recordings,
    isLoading,
    isError,
    refetch,
  } = useQuery({
    queryKey: ['recordings', cameraId, date],
    queryFn: () => getRecordings({ camera_id: cameraId || undefined, from, to }),
  });

  const cameraName = (id: string): string =>
    cameras?.find((c) => c.id === id)?.name ?? id;

  const handlePlayerError = () => {
    notify('error', 'Файл записи отсутствует на диске');
    // Список перезагрузится: сервер уже удалил устаревшую строку при обращении к файлу.
    refetch();
  };

  return (
    <div className="container">
      <h1>Архив записей</h1>

      <div className="archive-controls">
        <label>
          Камера
          <select
            value={cameraId}
            onChange={(e) => {
              setCameraId(e.target.value);
              setSelected(null);
            }}
          >
            <option value="">Все камеры</option>
            {cameras?.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </label>

        <label>
          Дата
          <input
            type="date"
            value={date}
            onChange={(e) => {
              setDate(e.target.value);
              setSelected(null);
            }}
          />
        </label>
      </div>

      {selected && (
        <div className="archive-player">
          <video
            key={selected.id}
            controls
            autoPlay
            muted
            playsInline
            src={recordingFileUrl(selected.id)}
            onError={handlePlayerError}
          />
        </div>
      )}

      {isLoading && <div className="loading">Загрузка архива...</div>}
      {isError && <div className="error">Ошибка загрузки архива</div>}

      {recordings && recordings.length === 0 && !isLoading && (
        <div className="loading">Записей за выбранный день нет</div>
      )}

      {recordings && recordings.length > 0 && (
        <table className="camera-table">
          <thead>
            <tr>
              <th>Камера</th>
              <th>Начало</th>
              <th>Длительность</th>
              <th>Размер</th>
              <th>Действия</th>
            </tr>
          </thead>
          <tbody>
            {recordings.map((rec) => (
              <tr key={rec.id}>
                <td>{cameraName(rec.camera_id)}</td>
                <td>{formatDateTime(rec.started_at)}</td>
                <td>{formatDuration(rec)}</td>
                <td>{formatBytes(rec.size_bytes)}</td>
                <td>
                  <button className="btn-small" onClick={() => setSelected(rec)}>
                    Смотреть
                  </button>
                  <a className="link-download" href={recordingFileUrl(rec.id)} download>
                    Скачать
                  </a>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}