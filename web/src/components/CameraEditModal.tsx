import { useEffect, useState, type FormEvent } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { updateCamera } from '../api/client';
import { useToast } from './Toast';
import type { Camera, CameraStatus } from '../types/camera';
import './CameraEditModal.css';

interface CameraEditModalProps {
  camera: Camera | null;
  onClose: () => void;
}

// Модальное окно редактирования камеры.
export default function CameraEditModal({ camera, onClose }: CameraEditModalProps) {
  const queryClient = useQueryClient();
  const notify = useToast();

  const [name, setName] = useState('');
  const [rtspUri, setRtspUri] = useState('');
  const [location, setLocation] = useState('');
  const [status, setStatus] = useState<CameraStatus>('enabled');

  // Заполняем форму текущими значениями при открытии окна.
  useEffect(() => {
    if (camera) {
      setName(camera.name);
      setRtspUri(camera.rtsp_uri);
      setLocation(camera.location ?? '');
      setStatus(camera.status);
    }
  }, [camera]);

  const mutation = useMutation({
    mutationFn: () => {
      if (!camera) {
        throw new Error('camera is not selected');
      }
      return updateCamera(camera.id, {
        name: name.trim(),
        rtsp_uri: rtspUri.trim(),
        // Пустая строка очищает расположение.
        location: location.trim(),
        status,
      });
    },
    onSuccess: () => {
      notify('success', 'Камера обновлена');
      queryClient.invalidateQueries({ queryKey: ['cameras'] });
      onClose();
    },
    onError: (error: Error) => {
      notify('error', error.message);
    },
  });

  if (!camera) {
    return null;
  }

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    mutation.mutate();
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-card" onClick={(e) => e.stopPropagation()}>
        <h2>Изменить камеру</h2>

        <form className="modal-form" onSubmit={handleSubmit}>
          <label>
            Название
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </label>

          <label>
            RTSP URI
            <input
              value={rtspUri}
              onChange={(e) => setRtspUri(e.target.value)}
              required
            />
          </label>

          <label>
            Расположение
            <input
              value={location}
              onChange={(e) => setLocation(e.target.value)}
            />
          </label>

          <label>
            Статус
            <select
              value={status}
              onChange={(e) => setStatus(e.target.value as CameraStatus)}
            >
              <option value="enabled">Включена</option>
              <option value="disabled">Отключена</option>
            </select>
          </label>

          <div className="modal-actions">
            <button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? 'Сохранение...' : 'Сохранить'}
            </button>
            <button
              type="button"
              className="btn-secondary"
              disabled={mutation.isPending}
              onClick={onClose}
            >
              Отмена
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}