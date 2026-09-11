import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getRecordingBlob, getRecordings } from '../api/client';
import type { SystemEvent } from '../types/event';
import './MotionClipModal.css';

interface MotionClipModalProps {
  event: SystemEvent | null;
  onClose: () => void;
}

// queryMargin — запас окна запроса: сегмент может начаться задолго до эпизода.
const queryMargin = 10 * 60_000;

function parsePayloadTime(value: unknown): Date | null {
  if (typeof value !== 'string') return null;
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? null : d;
}

// Модальное окно просмотра фрагмента архива по событию движения.
export default function MotionClipModal({ event, onClose }: MotionClipModalProps) {
  const [index, setIndex] = useState(0);
  const [videoUrl, setVideoUrl] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  // Границы эпизода движения из payload события.
  const episode = useMemo(() => {
    if (!event) return null;
    const start =
      parsePayloadTime(event.payload?.started_at) ?? new Date(event.occurred_at);
    const end = parsePayloadTime(event.payload?.ended_at) ?? start;
    return { start, end };
  }, [event]);

  const { data: recordings, isLoading } = useQuery({
    queryKey: ['recordings', 'clip', event?.id],
    queryFn: () =>
      getRecordings({
        camera_id: event?.camera_id,
        from: new Date((episode?.start.getTime() ?? 0) - queryMargin).toISOString(),
        to: new Date((episode?.end.getTime() ?? 0) + queryMargin).toISOString(),
      }),
    enabled: !!event && !!event.camera_id && !!episode,
  });

  // Плейлист: сегменты, пересекающие окно эпизода (предзапись 5 с, запас 30 с).
  const playlist = useMemo(() => {
    if (!recordings || !episode) return [];

    const from = new Date(episode.start.getTime() - 5_000);
    const to = new Date(episode.end.getTime() + 30_000);

    const overlapping = recordings.filter((r) => {
      const rStart = new Date(r.started_at);
      const rEnd = r.ended_at ? new Date(r.ended_at) : new Date();
      return rStart <= to && rEnd >= from;
    });

    const kept = overlapping.filter((r) => r.kept);
    const source = kept.length > 0 ? kept : overlapping;

    return source
      .slice()
      .sort(
        (a, b) => new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
      );
  }, [recordings, episode]);

  // Сбрасываем плейлист при смене события.
  useEffect(() => {
    setIndex(0);
  }, [event]);

  // Загружаем текущий сегмент как blob и освобождаем память при смене.
  useEffect(() => {
    let cancelled = false;
    let url: string | null = null;

    const rec = playlist[index];
    if (!rec) {
      setVideoUrl(null);
      return;
    }

    setLoading(true);
    getRecordingBlob(rec.id)
      .then((blob) => {
        if (cancelled) return;
        url = URL.createObjectURL(blob);
        setVideoUrl(url);
      })
      .catch(() => {
        if (!cancelled) setVideoUrl(null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [playlist, index]);

  if (!event || !episode) {
    return null;
  }

  const current = playlist[index];

  const handleEnded = () => {
    if (index + 1 < playlist.length) {
      setIndex(index + 1);
    }
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div
        className="modal-card modal-card-wide"
        onClick={(e) => e.stopPropagation()}
      >
        <h2>Фрагмент движения</h2>

        <p className="clip-meta">
          {new Date(episode.start).toLocaleString()} —{' '}
          {new Date(episode.end).toLocaleString()}
          {playlist.length > 0 && (
            <>
              {' '}· сегмент {index + 1} из {playlist.length}
            </>
          )}
        </p>

        {/* Индикатор загрузки: список сегментов или сам файл. */}
        {(isLoading || loading) && (
          <div className="loading">Загрузка записи...</div>
        )}

        {!isLoading && !loading && playlist.length === 0 && (
          <div className="error">Запись для этого события не найдена</div>
        )}

        {!isLoading && !loading && current && videoUrl && (
          <video
            key={current.id}
            className="clip-player"
            controls
            autoPlay
            muted
            playsInline
            src={videoUrl}
            onEnded={handleEnded}
          />
        )}

        <div className="modal-actions">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Закрыть
          </button>
        </div>
      </div>
    </div>
  );
}