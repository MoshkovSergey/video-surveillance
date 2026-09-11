import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getCameras, getEvents } from '../api/client';
import MotionClipModal from '../components/MotionClipModal';
import type { SystemEvent } from '../types/event';
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

function formatPayload(event: SystemEvent): string {
  const p = event.payload;
  if (!p || typeof p !== 'object') return '';

  switch (event.type) {
    case 'motion':
      if (typeof p.duration_sec === 'number') {
        return `Длительность: ${p.duration_sec} с`;
      }
      return 'Эпизод движения';

    case 'camera_online':
      return 'Связь восстановлена';

    case 'camera_offline':
      if (typeof p.reason === 'string') {
        return `Причина: ${p.reason}`;
      }
      return 'Камера недоступна';

    case 'fire_alarm':
      return 'Сработала пожарная сигнализация';

    case 'smoke_detection':
      return 'Обнаружен дым';

    case 'manual_alarm':
      if (typeof p.note === 'string') {
        return p.note;
      }
      return 'Ручная тревога';

    case 'recording_error':
      if (typeof p.error === 'string') {
        return `Ошибка: ${p.error}`;
      }
      return 'Ошибка записи';

    default:
      return JSON.stringify(p).slice(0, 60);
  }
}

export default function EventsPage() {
  const [cameraId, setCameraId] = useState('');
  const [type, setType] = useState('');
  const [clipEvent, setClipEvent] = useState<SystemEvent | null>(null);

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
              <th>Примечание</th>
              <th>Важность</th>
            </tr>
          </thead>
          <tbody>
            {events.map((ev) => (
              <tr key={ev.id}>
                <td>{new Date(ev.occurred_at).toLocaleString()}</td>
                <td>{cameraName(ev.camera_id)}</td>
                <td>
                  <span className={`event-type event-type-${ev.type}`}>
                    {eventTypeLabels[ev.type] ?? ev.type}
                  </span>
                </td>
                <td className="event-note">{formatPayload(ev)}</td>
                <td>
                  <span className={`severity-badge sev-${ev.severity}`}>
                    {severityLabels[ev.severity] ?? ev.severity}
                  </span>
                  {ev.type === 'motion' && (
                    <button
                      className="btn-small btn-clip"
                      onClick={() => setClipEvent(ev)}
                      title="Просмотреть фрагмент архива"
                    >
                      Просмотр
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <MotionClipModal event={clipEvent} onClose={() => setClipEvent(null)} />
    </div>
  );
}