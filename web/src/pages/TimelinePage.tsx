import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  getCameras,
  getEvents,
  getRecordingBlob,
  getRecordings,
} from '../api/client';
import type { Recording } from '../types/recording';
import './TimelinePage.css';

const DAY_MS = 24 * 60 * 60 * 1000;
const MIN_SPAN = 60_000; // минимальное окно масштаба: 1 минута
const GUTTER = 140; // ширина колонки имён камер, px

const fmtTime = (t: number) =>
  new Date(t).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' });

const fmtTimeSec = (t: number) =>
  new Date(t).toLocaleTimeString('ru-RU', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });

const fmtFull = (t: number) =>
  new Date(t).toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });

interface ViewWindow {
  s: number;
  e: number;
}

// Таймлайн архива: визуальные сутки по камерам с маркерами движения
// и клипов. Колесо мыши над лентой масштабирует окно с якорем в курсоре,
// перетаскивание линейки влево/вправо панорамирует окно,
// перетаскивание по дорожкам выбирает момент для плеера.
export default function TimelinePage() {
  const [date, setDate] = useState(() => new Date().toISOString().slice(0, 10));
  const [view, setView] = useState<ViewWindow | null>(null);
  const [playhead, setPlayhead] = useState<number | null>(null);
  const [currentRec, setCurrentRec] = useState<Recording | null>(null);
  const [videoUrl, setVideoUrl] = useState<string | null>(null);
  const [loadingVideo, setLoadingVideo] = useState(false);

  const bodyRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const dragRef = useRef(false);
  const panRef = useRef<{ x: number; s: number; e: number } | null>(null);

  const dayStart = useMemo(() => new Date(`${date}T00:00:00`).getTime(), [date]);
  const dayEnd = dayStart + DAY_MS;
  const from = new Date(dayStart).toISOString();
  const to = new Date(dayEnd).toISOString();

  const viewStart = view?.s ?? dayStart;
  const viewEnd = view?.e ?? dayEnd;
  const span = viewEnd - viewStart;

  const { data: cameras } = useQuery({ queryKey: ['cameras'], queryFn: getCameras });

  const { data: recordings, isLoading } = useQuery({
    queryKey: ['recordings', date],
    queryFn: () => getRecordings({ from, to }),
  });

  const { data: events } = useQuery({
    queryKey: ['events-motion', date],
    queryFn: () => getEvents({ type: 'motion', from, to, limit: 500 }),
  });

  // Смена даты сбрасывает масштаб и курсор.
  useEffect(() => {
    setView(null);
    setPlayhead(null);
    setCurrentRec(null);
  }, [date]);

  const pct = (t: number) => ((t - viewStart) / span) * 100;

  // Адаптивный шаг делений шкалы.
  const tickStep = useMemo(() => {
    const steps = [60_000, 5 * 60_000, 15 * 60_000, 30 * 60_000, 3_600_000, 2 * 3_600_000];
    for (const st of steps) {
      if (span / st <= 12) return st;
    }
    return 4 * 3_600_000;
  }, [span]);

  const ticks = useMemo(() => {
    const first = Math.ceil(viewStart / tickStep) * tickStep;
    const arr: number[] = [];
    for (let t = first; t <= viewEnd; t += tickStep) arr.push(t);
    return arr;
  }, [viewStart, viewEnd, tickStep]);

  // Зум с якорем в доле frac видимого окна.
  const applyZoom = (prev: ViewWindow | null, frac: number, factor: number): ViewWindow | null => {
    const s0 = prev?.s ?? dayStart;
    const e0 = prev?.e ?? dayEnd;
    const t = s0 + frac * (e0 - s0);

    let s = t - (t - s0) * factor;
    let e = t + (e0 - t) * factor;

    if (e - s < MIN_SPAN) {
      const c = (s + e) / 2;
      s = c - MIN_SPAN / 2;
      e = c + MIN_SPAN / 2;
    }
    if (s < dayStart) s = dayStart;
    if (e > dayEnd) e = dayEnd;
    if (e - s >= DAY_MS - 1000) return null;
    return { s, e };
  };

  // Панорамирование окна на 15% ширины (Shift+колесо).
  const applyPan = (prev: ViewWindow | null, dir: number): ViewWindow | null => {
    const s0 = prev?.s ?? dayStart;
    const e0 = prev?.e ?? dayEnd;
    if (e0 - s0 >= DAY_MS - 1000) return prev;

    const shift = (e0 - s0) * 0.15 * dir;
    let s = s0 + shift;
    let e = e0 + shift;
    if (s < dayStart) {
      e += dayStart - s;
      s = dayStart;
    }
    if (e > dayEnd) {
      s -= e - dayEnd;
      e = dayEnd;
    }
    return { s, e };
  };

  // Колесо мыши над полотном ленты: зум с якорем в курсоре; Shift+колесо — панорама.
  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return;

    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      e.stopPropagation();

      const rect = el.getBoundingClientRect();
      const trackLeft = rect.left + GUTTER;
      const trackWidth = rect.width - GUTTER;
      const frac = Math.min(1, Math.max(0, (e.clientX - trackLeft) / trackWidth));

      if (e.shiftKey) {
        setView((prev) => applyPan(prev, e.deltaY > 0 ? 1 : -1));
        return;
      }
      const factor = e.deltaY > 0 ? 1.25 : 0.8;
      setView((prev) => applyZoom(prev, frac, factor));
    };

    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dayStart, dayEnd]);

  // ---------- Панорамирование перетаскиванием линейки ----------

  const onRulerPointerDown = (e: React.PointerEvent) => {
    e.preventDefault();
    e.stopPropagation();
    (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
    panRef.current = { x: e.clientX, s: viewStart, e: viewEnd };
  };

  const onRulerPointerMove = (e: React.PointerEvent) => {
    const p = panRef.current;
    if (!p) return;

    const rect = bodyRef.current?.getBoundingClientRect();
    if (!rect) return;
    const trackWidth = rect.width - GUTTER;

    // Тянем содержимое за курсором: движение мыши вправо сдвигает окно влево.
    const dt = ((e.clientX - p.x) / trackWidth) * (p.e - p.s);
    let s = p.s - dt;
    let en = p.e - dt;

    if (s < dayStart) {
      en += dayStart - s;
      s = dayStart;
    }
    if (en > dayEnd) {
      s -= en - dayEnd;
      en = dayEnd;
    }

    setView(en - s >= DAY_MS - 1000 ? null : { s, e: en });
  };

  const onRulerPointerUp = (e: React.PointerEvent) => {
    (e.target as HTMLElement).releasePointerCapture?.(e.pointerId);
    panRef.current = null;
  };

  // ---------- Выбор момента перетаскиванием по дорожкам ----------

  const recEnd = (r: Recording) => (r.ended_at ? Date.parse(r.ended_at) : Date.now());

  const covering = (t: number): Recording | null =>
    (recordings ?? []).find((r) => {
      const s = Date.parse(r.started_at);
      return s <= t && t < recEnd(r);
    }) ?? null;

  const loadRec = async (rec: Recording, t: number) => {
    setLoadingVideo(true);
    try {
      const blob = await getRecordingBlob(rec.id);
      const url = URL.createObjectURL(blob);
      setVideoUrl((prev) => {
        if (prev) URL.revokeObjectURL(prev);
        return url;
      });
      setCurrentRec(rec);
      requestAnimationFrame(() => {
        const v = videoRef.current;
        if (v) {
          v.currentTime = Math.max(0, (t - Date.parse(rec.started_at)) / 1000);
        }
      });
    } finally {
      setLoadingVideo(false);
    }
  };

  const moveTo = (t: number, seekVideo: boolean) => {
    const clamped = Math.min(dayEnd - 1, Math.max(dayStart, t));
    setPlayhead(clamped);

    const rec = covering(clamped);
    if (!rec) {
      setCurrentRec(null);
      return;
    }
    if (rec.id !== currentRec?.id) {
      void loadRec(rec, clamped);
      return;
    }
    if (seekVideo && videoRef.current) {
      videoRef.current.currentTime = Math.max(0, (clamped - Date.parse(rec.started_at)) / 1000);
    }
  };

  const timeFromPointer = (clientX: number): number => {
    const rect = bodyRef.current?.getBoundingClientRect();
    if (!rect) return viewStart;
    const trackWidth = rect.width - GUTTER;
    const frac = (clientX - (rect.left + GUTTER)) / trackWidth;
    return viewStart + frac * span;
  };

  const onPointerDown = (e: React.PointerEvent) => {
    dragRef.current = true;
    (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
    moveTo(timeFromPointer(e.clientX), false);
  };
  const onPointerMove = (e: React.PointerEvent) => {
    if (dragRef.current) moveTo(timeFromPointer(e.clientX), false);
  };
  const onPointerUp = (e: React.PointerEvent) => {
    if (!dragRef.current) return;
    dragRef.current = false;
    moveTo(timeFromPointer(e.clientX), true);
  };

  return (
    <div className="container">
      <div className="tl-header">
        <h1>Таймлайн архива</h1>

        <div className="tl-zoom">
          <button className="btn-probe" onClick={() => setView((p) => applyZoom(p, 0.5, 1.25))} title="Отдалить">
            −
          </button>
          <button className="btn-probe" onClick={() => setView((p) => applyZoom(p, 0.5, 0.8))} title="Приблизить">
            +
          </button>
          <button className="btn-probe" onClick={() => setView(null)} title="Весь день">
            24 ч
          </button>
          <span className="tl-window">
            {fmtTime(viewStart)} — {fmtTime(viewEnd)}
          </span>
        </div>

        <input
          type="date"
          className="tl-date"
          value={date}
          max={new Date().toISOString().slice(0, 10)}
          onChange={(e) => setDate(e.target.value)}
        />
      </div>

      <div className="tl-legend">
        <span><i className="lg lg-buffer" /> сегмент буфера</span>
        <span><i className="lg lg-kept" /> клип (хранится)</span>
        <span><i className="lg lg-motion" /> событие движения</span>
        <span className="tl-hint">
          колесо — масштаб, тянуть шкалу — прокрутка окна, тянуть дорожки — выбор момента
        </span>
      </div>

      {isLoading && <div className="loading">Загрузка суток...</div>}

      <div
        className="tl-body"
        ref={bodyRef}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
      >
        <div
          className="tl-ruler"
          onPointerDown={onRulerPointerDown}
          onPointerMove={onRulerPointerMove}
          onPointerUp={onRulerPointerUp}
          onPointerCancel={onRulerPointerUp}
          title="Зажмите и тяните влево/вправо для прокрутки окна"
        >
          <div className="tl-ruler-spacer" />
          <div className="tl-ruler-track">
            {ticks.map((t) => (
              <span
                key={t}
                className={`tl-tick ${t % (4 * 3_600_000) === 0 ? 'tl-tick-major' : ''}`}
                style={{ left: `${pct(t)}%` }}
              >
                {tickStep <= 60_000 ? fmtTimeSec(t) : fmtTime(t)}
              </span>
            ))}
          </div>
        </div>

        {(cameras ?? []).map((cam) => {
          const recs = (recordings ?? []).filter((r) => r.camera_id === cam.id);
          const evs = (events ?? []).filter((ev) => ev.camera_id === cam.id);
          return (
            <div className="tl-lane" key={cam.id}>
              <div className="tl-lane-name" title={cam.name}>
                {cam.name}
              </div>
              <div className="tl-lane-track">
                {recs.map((r) => {
                  const s = Date.parse(r.started_at);
                  const e = Math.min(dayEnd, recEnd(r));
                  const left = pct(s);
                  const width = pct(e) - left;
                  if (left > 100 || left + width < 0) return null;
                  return (
                    <div
                      key={r.id}
                      className={`tl-seg ${r.kept ? 'tl-seg-kept' : 'tl-seg-buffer'}`}
                      style={{ left: `${left}%`, width: `${Math.max(0.2, width)}%` }}
                      title={`${fmtFull(s)} — ${r.ended_at ? fmtFull(e) : 'пишется'}${r.kept ? ' • клип' : ''}`}
                      onPointerDown={(ev) => ev.stopPropagation()}
                      onClick={(ev) => {
                        ev.stopPropagation();
                        moveTo(s + 500, true);
                      }}
                    />
                  );
                })}
                {evs.map((ev) => {
                  const left = pct(Date.parse(ev.occurred_at));
                  if (left < 0 || left > 100) return null;
                  return (
                    <span
                      key={ev.id}
                      className="tl-marker"
                      style={{ left: `${left}%` }}
                      title={`Движение: ${fmtFull(Date.parse(ev.occurred_at))}`}
                      onPointerDown={(e) => e.stopPropagation()}
                      onClick={(e) => {
                        e.stopPropagation();
                        moveTo(Date.parse(ev.occurred_at), true);
                      }}
                    />
                  );
                })}
              </div>
            </div>
          );
        })}

        {playhead !== null && playhead >= viewStart && playhead <= viewEnd && (
          <div
            className="tl-playhead"
            style={{ left: `calc(${GUTTER}px + (100% - ${GUTTER}px) * ${pct(playhead) / 100})` }}
          >
            <span className="tl-playhead-time">{fmtTimeSec(playhead)}</span>
          </div>
        )}
      </div>

      <div className="tl-player-wrap">
        {loadingVideo && <div className="loading">Загрузка фрагмента...</div>}
        {!loadingVideo && videoUrl && currentRec && (
          <>
            <div className="tl-player-title">
              {currentRec.kept ? 'Клип' : 'Сегмент'}: {fmtFull(Date.parse(currentRec.started_at))}
              {playhead !== null && <span className="tl-player-pos"> • курсор: {fmtFull(playhead)}</span>}
            </div>
            <video ref={videoRef} src={videoUrl} controls className="tl-player" />
          </>
        )}
        {!loadingVideo && !videoUrl && (
          <div className="tl-player-empty">
            Выберите момент на ленте: клик по сегменту, маркеру движения или перетаскивание курсора.
          </div>
        )}
      </div>
    </div>
  );
}