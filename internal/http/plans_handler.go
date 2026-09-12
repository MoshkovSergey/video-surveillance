package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
	"gitverse.ru/cataclysm78/video-surveillance/internal/postgres"
)

// maxPlanImageBytes — предел размера загружаемой схемы (32 МБ).
const maxPlanImageBytes = 32 << 20

type planDTO struct {
	ID        uuid.UUID         `json:"id"`
	Name      string            `json:"name"`
	ImageURL  string            `json:"image_url"`
	CreatedAt string            `json:"created_at"`
	Objects   []planObjectDTO   `json:"objects,omitempty"`
}

type planObjectDTO struct {
	ID         uuid.UUID  `json:"id"`
	Kind       string     `json:"kind"`
	CameraID   *uuid.UUID `json:"camera_id"`
	CameraName string     `json:"camera_name,omitempty"`
	Label      string     `json:"label"`
	X          float64    `json:"x"`
	Y          float64    `json:"y"`
}

type putObjectsRequest struct {
	Objects []planObjectDTO `json:"objects"`
}

func (h *Handler) floorPlanRepo() *postgres.FloorPlanRepository {
	return postgres.NewFloorPlanRepository(h.pool)
}

func (h *Handler) plansDir() string {
	return filepath.Join(h.storageRoot, "plans")
}

// handleListPlans возвращает список планов объекта.
func (h *Handler) handleListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.floorPlanRepo().List(r.Context())
	if err != nil {
		h.logger.Error("failed to list floor plans", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	out := make([]planDTO, 0, len(plans))
	for _, p := range plans {
		out = append(out, planDTO{
			ID:        p.ID,
			Name:      p.Name,
			ImageURL:  "/api/v1/plans/" + p.ID.String() + "/image",
			CreatedAt: p.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreatePlan загружает схему и создает план (multipart: name, image).
func (h *Handler) handleCreatePlan(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxPlanImageBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректная форма загрузки"})
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "укажите наименование плана"})
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "приложите файл схемы (image)"})
		return
	}
	defer file.Close()

	if header.Size > maxPlanImageBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "файл схемы больше 32 МБ"})
		return
	}

	mime := header.Header.Get("Content-Type")
	var ext string
	switch mime {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "допустимы только PNG или JPEG"})
		return
	}

	planID := uuid.New()
	if err := os.MkdirAll(h.plansDir(), 0o755); err != nil {
		h.logger.Error("failed to create plans dir", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	rel := filepath.Join("plans", planID.String()+ext)
	dst, err := os.Create(filepath.Join(h.storageRoot, rel))
	if err != nil {
		h.logger.Error("failed to create plan file", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		h.logger.Error("failed to write plan file", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	plan := &domain.FloorPlan{
		ID:        planID,
		Name:      name,
		ImagePath: filepath.ToSlash(rel),
		ImageMime: mime,
	}
	if err := h.floorPlanRepo().Create(r.Context(), plan); err != nil {
		h.logger.Error("failed to create floor plan", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	writeJSON(w, http.StatusOK, planDTO{
		ID:        plan.ID,
		Name:      plan.Name,
		ImageURL:  "/api/v1/plans/" + plan.ID.String() + "/image",
		CreatedAt: plan.CreatedAt.Format("2006-01-02 15:04:05"),
	})
}

// handleGetPlan возвращает план вместе с объектами и именами камер.
func (h *Handler) handleGetPlan(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор плана"})
		return
	}

	plan, err := h.floorPlanRepo().Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "план не найден"})
		return
	}

	objects, err := h.floorPlanRepo().ListObjects(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to list plan objects", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	names := make(map[uuid.UUID]string)
	if cams, err := h.cameraRepo.List(r.Context()); err == nil {
		for _, cam := range cams {
			names[cam.ID] = cam.Name
		}
	}

	dto := planDTO{
		ID:        plan.ID,
		Name:      plan.Name,
		ImageURL:  "/api/v1/plans/" + plan.ID.String() + "/image",
		CreatedAt: plan.CreatedAt.Format("2006-01-02 15:04:05"),
		Objects:   make([]planObjectDTO, 0, len(objects)),
	}
	for _, obj := range objects {
		item := planObjectDTO{
			ID:       obj.ID,
			Kind:     string(obj.Kind),
			CameraID: obj.CameraID,
			Label:    obj.Label,
			X:        obj.X,
			Y:        obj.Y,
		}
		if obj.CameraID != nil {
			item.CameraName = names[*obj.CameraID]
		}
		dto.Objects = append(dto.Objects, item)
	}

	writeJSON(w, http.StatusOK, dto)
}

// handleGetPlanImage отдает файл схемы плана.
func (h *Handler) handleGetPlanImage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор плана"})
		return
	}

	plan, err := h.floorPlanRepo().Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "план не найден"})
		return
	}

	host := filepath.Join(h.storageRoot, filepath.FromSlash(plan.ImagePath))
	if _, err := os.Stat(host); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "файл схемы не найден"})
		return
	}

	w.Header().Set("Content-Type", plan.ImageMime)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, host)
}

// handleRenamePlan меняет наименование плана.
func (h *Handler) handleRenamePlan(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор плана"})
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "укажите наименование плана"})
		return
	}

	if err := h.floorPlanRepo().Rename(r.Context(), id, strings.TrimSpace(req.Name)); err != nil {
		h.logger.Error("failed to rename floor plan", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "renamed"})
}

// handleDeletePlan удаляет план и его файл схемы.
func (h *Handler) handleDeletePlan(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор плана"})
		return
	}

	plan, err := h.floorPlanRepo().Get(r.Context(), id)
	if err == nil {
		_ = os.Remove(filepath.Join(h.storageRoot, filepath.FromSlash(plan.ImagePath)))
	}

	if err := h.floorPlanRepo().Delete(r.Context(), id); err != nil {
		h.logger.Error("failed to delete floor plan", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handlePutPlanObjects атомарно сохраняет размещение объектов плана.
func (h *Handler) handlePutPlanObjects(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор плана"})
		return
	}

	var req putObjectsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректное тело запроса"})
		return
	}

	// Проверяем существование камер и валидность координат.
	cams := make(map[uuid.UUID]bool)
	if list, err := h.cameraRepo.List(r.Context()); err == nil {
		for _, cam := range list {
			cams[cam.ID] = true
		}
	}

	objects := make([]domain.PlanObject, 0, len(req.Objects))
	for _, item := range req.Objects {
		kind := domain.PlanObjectKind(item.Kind)
		if kind != domain.PlanObjectCamera && kind != domain.PlanObjectExit && kind != domain.PlanObjectZone {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "недопустимый тип объекта: " + item.Kind})
			return
		}
		if kind == domain.PlanObjectCamera {
			if item.CameraID == nil || !cams[*item.CameraID] {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "камера объекта не найдена"})
				return
			}
		}
		if item.X < 0 || item.X > 100 || item.Y < 0 || item.Y > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "координаты объекта вне диапазона 0..100"})
			return
		}

		objects = append(objects, domain.PlanObject{
			ID:       item.ID,
			Kind:     kind,
			CameraID: item.CameraID,
			Label:    item.Label,
			X:        item.X,
			Y:        item.Y,
		})
	}

	if err := h.floorPlanRepo().ReplaceObjects(r.Context(), id, objects); err != nil {
		h.logger.Error("failed to save plan objects", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "saved", "count": len(objects)})
}

// handlePlanStatus возвращает онлайн-статус камер плана.
func (h *Handler) handlePlanStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор плана"})
		return
	}

	objects, err := h.floorPlanRepo().ListObjects(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to list plan objects", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	states, err := h.eventRepo.LatestCameraStates(r.Context())
	if err != nil {
		h.logger.Error("failed to get camera states", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	out := make(map[string]bool)
	for _, obj := range objects {
		if obj.Kind != domain.PlanObjectCamera || obj.CameraID == nil {
			continue
		}
		out[obj.CameraID.String()] = states[*obj.CameraID]
	}

	writeJSON(w, http.StatusOK, map[string]any{"cameras": out})
}