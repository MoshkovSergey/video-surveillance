import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getCameras } from "../api/client";
import {
  deletePlan,
  getPlan,
  getPlanStatus,
  putPlanObjects,
  renamePlan,
} from "../api/plans";
import { useAuth } from "../auth/AuthContext";
import { useToast } from "../components/Toast";
import type { PlanObjectDTO } from "../types/plan";
import "./PlansPage.css";
import PlanImage from "../components/PlanImage";

// Просмотр схемы объекта; редактирование (перетаскивание, добавление,
// удаление объектов) — только для администратора.
export default function PlanViewPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const notify = useToast();
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";

  const canvasRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<string | null>(null);

  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<PlanObjectDTO[] | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [addCameraId, setAddCameraId] = useState("");

  const { data: plan, isLoading } = useQuery({
    queryKey: ["plan", id],
    queryFn: () => getPlan(id as string),
    enabled: !!id,
  });

  const { data: status } = useQuery({
    queryKey: ["plan-status", id],
    queryFn: () => getPlanStatus(id as string),
    enabled: !!id,
    refetchInterval: 10_000,
  });

  const { data: cameras } = useQuery({
    queryKey: ["cameras"],
    queryFn: getCameras,
  });

  const objects = editing && draft ? draft : (plan?.objects ?? []);

  const saveMutation = useMutation({
    mutationFn: () =>
      putPlanObjects(
        id as string,
        (draft ?? []).map((o) => ({
          id: o.id,
          kind: o.kind,
          camera_id: o.camera_id,
          label: o.label,
          x: o.x,
          y: o.y,
        })),
      ),
    onSuccess: () => {
      notify("success", "План сохранён");
      setEditing(false);
      setSelected(null);
      queryClient.invalidateQueries({ queryKey: ["plan", id] });
    },
    onError: (error: Error) => notify("error", error.message),
  });

  const startEdit = () => {
    setDraft((plan?.objects ?? []).map((o) => ({ ...o })));
    setSelected(null);
    setEditing(true);
  };

  const cancelEdit = () => {
    setDraft(null);
    setSelected(null);
    setEditing(false);
  };

  // Перетаскивание объектов в режиме редактирования.
  useEffect(() => {
    if (!editing) return;

    const move = (ev: MouseEvent) => {
      const dragId = dragRef.current;
      const rect = canvasRef.current?.getBoundingClientRect();
      if (!dragId || !rect) return;

      const x = Math.min(
        100,
        Math.max(0, ((ev.clientX - rect.left) / rect.width) * 100),
      );
      const y = Math.min(
        100,
        Math.max(0, ((ev.clientY - rect.top) / rect.height) * 100),
      );

      setDraft((prev) =>
        (prev ?? []).map((o) =>
          o.id === dragId
            ? { ...o, x: Math.round(x * 10) / 10, y: Math.round(y * 10) / 10 }
            : o,
        ),
      );
    };
    const up = () => {
      dragRef.current = null;
    };

    window.addEventListener("mousemove", move);
    window.addEventListener("mouseup", up);
    return () => {
      window.removeEventListener("mousemove", move);
      window.removeEventListener("mouseup", up);
    };
  }, [editing]);

  const addCamera = () => {
    const cam = cameras?.find((c) => c.id === addCameraId);
    if (!cam) return;
    setDraft((prev) => [
      ...(prev ?? []),
      {
        id: crypto.randomUUID(),
        kind: "camera",
        camera_id: cam.id,
        camera_name: cam.name,
        label: cam.name,
        x: 50,
        y: 50,
      },
    ]);
    setAddCameraId("");
  };

  const addSimple = (kind: "exit" | "zone") => {
    const label = window.prompt(
      kind === "exit" ? "Наименование выхода" : "Наименование зоны",
      "",
    );
    if (label === null) return;
    setDraft((prev) => [
      ...(prev ?? []),
      {
        id: crypto.randomUUID(),
        kind,
        camera_id: null,
        label: label.trim() || (kind === "exit" ? "Выход" : "Зона"),
        x: 50,
        y: 50,
      },
    ]);
  };

  const deleteSelected = () => {
    if (!selected) return;
    setDraft((prev) => (prev ?? []).filter((o) => o.id !== selected));
    setSelected(null);
  };

  const doRename = () => {
    if (!plan) return;
    const name = window.prompt("Наименование плана", plan.name);
    if (!name || !name.trim()) return;
    renamePlan(plan.id, name.trim())
      .then(() => {
        notify("success", "Переименовано");
        queryClient.invalidateQueries({ queryKey: ["plan", id] });
        queryClient.invalidateQueries({ queryKey: ["plans"] });
      })
      .catch((e: Error) => notify("error", e.message));
  };

  const doDelete = () => {
    if (!plan) return;
    if (!window.confirm(`Удалить план «${plan.name}»?`)) return;
    deletePlan(plan.id)
      .then(() => {
        notify("success", "План удалён");
        navigate("/plans");
      })
      .catch((e: Error) => notify("error", e.message));
  };

  if (isLoading) {
    return (
      <div className="container">
        <div className="loading">Загрузка плана...</div>
      </div>
    );
  }

  if (!plan) {
    return (
      <div className="container">
        <div className="error">План не найден</div>
        <Link to="/plans">← К планам</Link>
      </div>
    );
  }

  return (
    <div className="container">
      <div className="plans-header">
        <Link to="/plans" className="view-back">
          ← К планам
        </Link>
        <h1>{plan.name}</h1>

        {isAdmin && !editing && (
          <div className="plan-toolbar">
            <button className="btn-probe" onClick={startEdit}>
              Редактировать
            </button>
            <button className="btn-probe" onClick={doRename}>
              Переименовать
            </button>
            <button className="btn-danger-small" onClick={doDelete}>
              Удалить
            </button>
          </div>
        )}

        {isAdmin && editing && (
          <div className="plan-toolbar">
            <select
              value={addCameraId}
              onChange={(e) => setAddCameraId(e.target.value)}
            >
              <option value="">— камера —</option>
              {(cameras ?? [])
                .filter((c) => !(draft ?? []).some((o) => o.camera_id === c.id))
                .map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
            </select>
            <button
              className="btn-probe"
              disabled={!addCameraId}
              onClick={addCamera}
            >
              + Камера
            </button>
            <button className="btn-probe" onClick={() => addSimple("exit")}>
              + Выход
            </button>
            <button className="btn-probe" onClick={() => addSimple("zone")}>
              + Зона
            </button>
            <button
              className="btn-danger-small"
              disabled={!selected}
              onClick={deleteSelected}
            >
              Удалить выбр.
            </button>
            <button
              className="btn-gradient"
              disabled={saveMutation.isPending}
              onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending ? "Сохранение..." : "Сохранить"}
            </button>
            <button className="btn-probe" onClick={cancelEdit}>
              Отмена
            </button>
          </div>
        )}
      </div>

      <div className="plan-canvas" ref={canvasRef}>
        <PlanImage
          id={plan.id}
          alt={plan.name}
          className="plan-canvas-image"
          draggable={false}
        />

        {objects.map((o) => {
          const online =
            o.kind === "camera" && o.camera_id
              ? status?.cameras[o.camera_id]
              : undefined;
          const icon =
            o.kind === "camera" ? "📹" : o.kind === "exit" ? "🚪" : "🔥";
          const title =
            (o.label || o.camera_name || o.kind) +
            (online === false
              ? " — КАМЕРА НЕДОСТУПНА"
              : online === true
                ? " — в сети"
                : "");

          return (
            <span
              key={o.id}
              className={[
                "plan-icon",
                `plan-icon-${o.kind}`,
                online === false ? "plan-icon-offline" : "",
                selected === o.id ? "plan-icon-selected" : "",
              ]
                .filter(Boolean)
                .join(" ")}
              style={{ left: `${o.x}%`, top: `${o.y}%` }}
              title={title}
              onMouseDown={(e) => {
                if (editing) {
                  e.preventDefault();
                  dragRef.current = o.id;
                  setSelected(o.id);
                }
              }}
              onClick={() => {
                if (editing) {
                  setSelected(o.id);
                  return;
                }
                if (o.kind === "camera" && o.camera_id) {
                  navigate(`/cameras/${o.camera_id}/view`);
                }
              }}
            >
              {icon}
            </span>
          );
        })}
      </div>

      <div className="plan-legend">
        <span>📹 камера (клик — живой поток)</span>
        <span>🚪 эвакуационный выход</span>
        <span>🔥 пожарная зона</span>
        <span className="plan-icon-offline-demo">
          красное мигание — камера недоступна
        </span>
        {editing && (
          <span>
            перетаскивайте иконки мышью; двойной клик в режиме правки не
            используется — удаление через кнопку
          </span>
        )}
      </div>
    </div>
  );
}
