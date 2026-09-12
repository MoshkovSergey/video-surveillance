import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getCameras, getRecordingBlob, getRecordings } from '../api/client';
import { useToast } from '../components/Toast';
import Pagination from '../components/Pagination';
import type { Recording } from '../types/recording';
import './ArchivePage.css';

const PAGE_SIZE = 20;

const fmtRec = (rec: Recording) => {
  const start = new Date(rec.started_at).toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
  const end = rec.ended_at
    ? new Date(rec.ended_at).toLocaleTimeString('ru-RU', {
        hour: '2-digit',
        minute: '2-digit',
      })
    : '—';
  const size = rec.size_bytes ? `${(rec.size_bytes / 1024 / 1024).toFixed(1)} МБ` : '—';
  return { start, end, size };
};

export default function ArchivePage() {
  const notify = useToast();
  const [page, setPage] = useState(1);
  const [opening, setOpening] = useState<string | null>(null);
  const [playing, setPlaying] = useState<{ url: string; title: string } | null>(null);
  const offset = (page - 1) * PAGE_SIZE;

  const { data: cameras } = useQuery({ queryKey: ['cameras'], queryFn: getCameras });
  const camName = (id: string) => (cameras ?? []).find((c) => c.id === id)?.name ?? id;

  const { data: recordings, isLoading } = useQuery({
    queryKey: ['recordings-archive', page],
    queryFn: () => getRecordings({ kept: true, limit: PAGE_SIZE + 1, offset }),
  });

  const hasMore = (recordings ?? []).length > PAGE_SIZE;
  const visible = (recordings ?? []).slice(0, PAGE_SIZE);
  const totalPages = hasMore ? page + 1 : page;

  // Загрузка клипа через авторизованный запрос и показ во встроенном плеере.
  const openRec = async (rec: Recording) => {
    setOpening(rec.id);
    try {
      const blob = await getRecordingBlob(rec.id);
      const url = URL.createObjectURL(blob);
      const { start } = fmtRec(rec);
      setPlaying({ url, title: `${camName(rec.camera_id)} • ${start}` });
    } catch (e) {
      notify('error', (e as Error).message);
    } finally {
      setOpening(null);
    }
  };

  const closePlayer = () => {
    setPlaying((p) => {
      if (p) URL.revokeObjectURL(p.url);
      return null;
    });
  };

  return (
    <div className="container">
      <h1>Архив клипов</h1>

      {isLoading && <div className="loading">Загрузка...</div>}

      <div className="archive-table">
        <div className="archive-header">
          <span>Начало</span>
          <span>Конец</span>
          <span>Размер</span>
          <span>Камера</span>
          <span></span>
        </div>
        {visible.map((rec) => {
          const { start, end, size } = fmtRec(rec);
          return (
            <div key={rec.id} className="archive-row">
              <span>{start}</span>
              <span>{end}</span>
              <span>{size}</span>
              <span>{camName(rec.camera_id)}</span>
              <button
                className="archive-link"
                disabled={opening === rec.id}
                onClick={() => openRec(rec)}
              >
                {opening === rec.id ? 'Загрузка...' : 'Открыть'}
              </button>
            </div>
          );
        })}
        {!isLoading && visible.length === 0 && (
          <div className="archive-empty">Клипов пока нет</div>
        )}
      </div>

      <Pagination current={page} total={totalPages} onPage={setPage} />

      {playing && (
        <div className="archive-modal" onClick={closePlayer}>
          <div className="archive-modal-box" onClick={(e) => e.stopPropagation()}>
            <div className="archive-modal-header">
              <span>{playing.title}</span>
              <button className="archive-modal-close" onClick={closePlayer} title="Закрыть">
                ✕
              </button>
            </div>
            <video src={playing.url} controls autoPlay className="archive-modal-video" />
          </div>
        </div>
      )}
    </div>
  );
}