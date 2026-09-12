import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  getCameras,
  getEvents,
  getRecordingBlob,
  getRecordings,
} from "../api/client";
import { useToast } from "../components/Toast";
import type { SystemEvent } from "../types/event";
import Pagination from "../components/Pagination";
import "./EventsPage.css";

const PAGE_SIZE = 20;

// Русские наименования типов событий.
const typeLabels: Record<string, string> = {
  motion: "Движение",
  camera_online: "Камера в сети",
  camera_offline: "Потеря связи с камерой",
  fire_alarm: "Пожарная тревога",
  smoke_detection: "Обнаружение дыма",
  manual_alarm: "Ручная тревога",
  recording_error: "Ошибка записи",
};

// Цветовые классы бейджей по типу события.
const typeClasses: Record<string, string> = {
  motion: "events-type-motion",
  camera_online: "events-type-online",
  camera_offline: "events-type-offline",
  fire_alarm: "events-type-alarm",
  smoke_detection: "events-type-alarm",
  manual_alarm: "events-type-alarm",
  recording_error: "events-type-error",
};

const fmtTimeFull = (iso: string) =>
  new Date(iso).toLocaleString("ru-RU", {
    day: "2-digit",
    month: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });

// Примечание из полезных полей события.
const buildNote = (ev: SystemEvent): string => {
  const p = (ev.payload ?? {}) as Record<string, unknown>;

  if (ev.type === "motion") {
    const parts: string[] = [];
    if (typeof p.duration_sec === "number")
      parts.push(`длительность ${p.duration_sec} с`);
    if (p.snapshot) parts.push("снимок есть");
    return parts.join(", ");
  }
  if (ev.type === "recording_error") {
    return String(p.error ?? p.message ?? "");
  }
  if (ev.type === "fire_alarm" || ev.type === "smoke_detection") {
    return String(p.source ?? "");
  }
  return "";
};

export default function EventsPage() {
  const notify = useToast();
  const [page, setPage] = useState(1);
  const [opening, setOpening] = useState<string | null>(null);
  const [playing, setPlaying] = useState<{ url: string; title: string } | null>(
    null,
  );
  const offset = (page - 1) * PAGE_SIZE;

  const { data: cameras } = useQuery({
    queryKey: ["cameras"],
    queryFn: getCameras,
  });
  const camName = (id: string | null | undefined) =>
    id ? ((cameras ?? []).find((c) => c.id === id)?.name ?? id) : "—";

  const {
    data: events,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["events", page],
    queryFn: () => getEvents({ limit: PAGE_SIZE + 1, offset }),
  });

  const hasMore = (events ?? []).length > PAGE_SIZE;
  const visible = (events ?? []).slice(0, PAGE_SIZE);
  const totalPages = hasMore ? page + 1 : page;

  // Открытие клипа по событию движения.
  const openMotion = async (ev: SystemEvent) => {
    if (!ev.camera_id) return;
    const p = (ev.payload ?? {}) as Record<string, unknown>;
    const start = p.started_at
      ? new Date(String(p.started_at))
      : new Date(ev.occurred_at);
    const end = p.ended_at
      ? new Date(String(p.ended_at))
      : new Date(ev.occurred_at);
    const from = new Date(start.getTime() - 60_000).toISOString();
    const to = new Date(end.getTime() + 60_000).toISOString();

    setOpening(ev.id);
    try {
      const recs = await getRecordings({
        camera_id: ev.camera_id,
        kept: true,
        from,
        to,
        limit: 1,
      });
      if (!recs || recs.length === 0) {
        notify("error", "Клип по этому событию не найден в архиве");
        return;
      }
      const blob = await getRecordingBlob(recs[0].id);
      const url = URL.createObjectURL(blob);
      setPlaying({
        url,
        title: `Движение • ${camName(ev.camera_id)} • ${fmtTimeFull(ev.occurred_at)}`,
      });
    } catch (e) {
      notify("error", (e as Error).message);
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
      <h1>События</h1>

      {isLoading && <div className="loading">Загрузка...</div>}

      {!isLoading && error && (
        <div className="events-empty">
          Ошибка загрузки: {(error as Error).message}
        </div>
      )}

      {!isLoading && !error && (
        <div className="events-table">
          <div className="events-header">
            <span>Время</span>
            <span>Тип</span>
            <span>Камера</span>
            <span>Примечание</span>
            <span></span>
          </div>

          {visible.map((ev) => (
            <div key={ev.id} className="events-row">
              <span>{fmtTimeFull(ev.occurred_at)}</span>
              <span>
                <i className={`events-type ${typeClasses[ev.type] ?? ""}`}>
                  {typeLabels[ev.type] ?? ev.type}
                </i>
              </span>
              <span>{camName(ev.camera_id)}</span>
              <span className="events-note">{buildNote(ev)}</span>
              <span className="events-actions">
                {ev.type === "motion" && (
                  <button
                    className="events-view-btn"
                    disabled={opening === ev.id}
                    onClick={() => openMotion(ev)}
                  >
                    {opening === ev.id ? "Загрузка..." : "Просмотр"}
                  </button>
                )}
              </span>
            </div>
          ))}

          {visible.length === 0 && (
            <div className="events-empty">Событий пока нет</div>
          )}
        </div>
      )}

      <Pagination current={page} total={totalPages} onPage={setPage} />

      {playing && (
        <div className="events-modal" onClick={closePlayer}>
          <div
            className="events-modal-box"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="events-modal-header">
              <span>{playing.title}</span>
              <button
                className="events-modal-close"
                onClick={closePlayer}
                title="Закрыть"
              >
                ✕
              </button>
            </div>
            <video
              src={playing.url}
              controls
              autoPlay
              className="events-modal-video"
            />
          </div>
        </div>
      )}
    </div>
  );
}
