import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getCameras, getEvents } from '../api/client';
import type { SystemEvent } from '../types/event';
import Pagination from '../components/Pagination';
import './EventsPage.css';

const PAGE_SIZE = 20;

const fmtEvent = (ev: SystemEvent) => {
  const time = new Date(ev.occurred_at).toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
  const type = ev.type.replace('_', ' ');
  return { time, type };
};

export default function EventsPage() {
  const [page, setPage] = useState(1);
  const offset = (page - 1) * PAGE_SIZE;

  const { data: cameras } = useQuery({ queryKey: ['cameras'], queryFn: getCameras });
  const camName = (id: string | null | undefined) =>
    id ? (cameras ?? []).find((c) => c.id === id)?.name ?? id : '—';

  const { data: events, isLoading } = useQuery({
    queryKey: ['events', page],
    queryFn: () => getEvents({ limit: PAGE_SIZE + 1, offset }),
  });

  const hasMore = (events ?? []).length > PAGE_SIZE;
  const visible = (events ?? []).slice(0, PAGE_SIZE);
  const totalPages = hasMore ? page + 1 : page;

  return (
    <div className="container">
      <h1>События</h1>

      {isLoading && <div className="loading">Загрузка...</div>}

      <div className="events-table">
        <div className="events-header">
          <span>Время</span>
          <span>Тип</span>
          <span>Камера</span>
        </div>
        {visible.map((ev) => {
          const { time, type } = fmtEvent(ev);
          return (
            <div key={ev.id} className="events-row">
              <span>{time}</span>
              <span className="events-type">{type}</span>
              <span>{camName(ev.camera_id)}</span>
            </div>
          );
        })}
        {!isLoading && visible.length === 0 && (
          <div className="events-empty">Событий пока нет</div>
        )}
      </div>

      <Pagination current={page} total={totalPages} onPage={setPage} />
    </div>
  );
}