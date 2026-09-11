import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getCameras, getEvents } from '../api/client';
import type { EventType } from '../types/event';
import './EventsPage.css';

const eventTypeLabels: Record<EventType, string> = {
  motion: 'Движение',
  camera_online: 'Камера в сети',
  camera_offline: 'Потеря связи с камерой',
  fire_alarm: 'Пожарная тревога',
  smoke_detection: 'Обнаружение дыма',
  manual_alarm: 'Ручная тревога',
  recording_error: 'Ошибка записи',
};

const severityLabels: Record<string, string> = {
  info: 'инфо',
  warning: 'внимание',
  critical: 'критично',
};

export default function EventsPage() {
  const [cameraId, setCameraId] = useState('');
  const [type, setType] = useState('');

  const { data: cameras } = useQuery({
    queryKey: ['cameras'],
    queryFn: getCameras,
  });

  const { data: events, isLoading, isError } = useQuery({
    queryKey: ['events', cameraId, type],
    queryFn: () =>
      getEvents({
        camera_id: cameraId || undefined,
        type: type || undefined,
        limit: 200,
      }),
    refetchInterval: 15_000,
  });

  const cameraName = (id?: string): string => {
    if (!id) return '—';
    return cameras?.find((c) => c.id === id)?.name ?? id;
  };

  return (
    <div className="container">
      <h1>Журнал событий</h1>

      <div className="archive-controls">
        <label>
          Камера
          <select value={cameraId} onChange={(e) => setCameraId(e.target.value)}>
            <option value="">Все камеры</option>
            {cameras?.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </label>

        <label>
          Тип события
          <select value={type} onChange={(e) => setType(e.target.value)}>
            <option value="">Все типы</option>
            {Object.entries(eventTypeLabels).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
      </div>

      {isLoading && <div className="loading">Загрузка журнала...</div>}
      {isError && <div className="error">Ошибка загрузки журнала</div>}

      {events && events.length === 0 && !isLoading && (
        <div className="loading">Событий не зафиксировано</div>
      )}

      {events && events.length > 0 && (
        <table className="camera-table">
          <thead>
            <tr>
              <th>Время</th>
              <th>Камера</th>
              <th>Событие</th>
              <th>Важность</th>
            </tr>
          </thead>
          <tbody>
            {events.map((ev) => (
              <tr key={ev.id}>
                <td>{new Date(ev.occurred_at).toLocaleString()}</td>
                <td>{cameraName(ev.camera_id)}</td>
                <td>{eventTypeLabels[ev.type] ?? ev.type}</td>
                <td>
                  <span className={`severity-badge sev-${ev.severity}`}>
                    {severityLabels[ev.severity] ?? ev.severity}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}