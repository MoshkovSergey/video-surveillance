import { useEffect, useState, type FormEvent } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { updateCamera } from '../api/client';
import { useToast } from './Toast';
import type {
  Camera,
  CameraSourceType,
  CameraStatus,
  RecordingMode,
} from '../types/camera';
import './CameraEditModal.css';

interface CameraEditModalProps {
  camera: Camera | null;
  onClose: () => void;
}

// Модальное окно редактирования камеры.
// Опрос профилей ONVIF здесь не выполняется: профиль сохраняется текущий,
// а RTSP-адрес бэкенд повторно получает с камеры при сохранении.
export default function CameraEditModal({ camera, onClose }: CameraEditModalProps) {
  const queryClient = useQueryClient();
  const notify = useToast();

  const [sourceType, setSourceType] = useState<CameraSourceType>('rtsp');

  const [name, setName] = useState('');
  const [rtspUri, setRtspUri] = useState('');
  const [location, setLocation] = useState('');
  const [status, setStatus] = useState<CameraStatus>('enabled');

  const [onvifHost, setOnvifHost] = useState('');
  const [onvifPort, setOnvifPort] = useState('80');
  const [onvifUsername, setOnvifUsername] = useState('');
  const [onvifPassword, setOnvifPassword] = useState('');

  const [recordingMode, setRecordingMode] = useState<RecordingMode>('continuous');
  const [motionDetection, setMotionDetection] = useState(false);

  useEffect(() => {
    if (!camera) return;

    setName(camera.name);
    setLocation(camera.location ?? '');
    setStatus(camera.status);
    setRecordingMode(camera.recording_mode ?? 'continuous');
    setMotionDetection(camera.motion_detection ?? false);

    const st = camera.source_type ?? 'rtsp';
    setSourceType(st);

    if (st === 'rtsp') {
      setRtspUri(camera.rtsp_uri);
    } else if (camera.onvif) {
      setOnvifHost(camera.onvif.host);
      setOnvifPort(String(camera.onvif.port ?? 80));
      setOnvifUsername(camera.onvif.username ?? '');
      setOnvifPassword(camera.onvif.password ?? '');
    }
  }, [camera]);

  const mutation = useMutation({
    mutationFn: () => {
      if (!camera) {
        throw new Error('camera is not selected');
      }

      const payload: Parameters<typeof updateCamera>[1] = {
        name: name.trim(),
        location: location.trim(),
        status,
        source_type: sourceType,
        recording_mode: recordingMode,
        motion_detection: motionDetection && sourceType === 'onvif',
      };

      if (sourceType === 'rtsp') {
        payload.rtsp_uri = rtspUri.trim();
      } else {
        payload.onvif = {
          host: onvifHost.trim(),
          port: parseInt(onvifPort, 10) || 80,
          username: onvifUsername.trim(),
          password: onvifPassword,
          profile: camera.onvif?.profile,
        };
      }

      return updateCamera(camera.id, payload);
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
          <div className="source-switcher">
            <button
              type="button"
              className={`source-btn ${sourceType === 'rtsp' ? 'active' : ''}`}
              onClick={() => setSourceType('rtsp')}
            >
              RTSP
            </button>
            <button
              type="button"
              className={`source-btn ${sourceType === 'onvif' ? 'active' : ''}`}
              onClick={() => setSourceType('onvif')}
            >
              ONVIF
            </button>
          </div>

          <label>
            Название
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
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

          {sourceType === 'rtsp' ? (
            <label>
              RTSP URI
              <input
                value={rtspUri}
                onChange={(e) => setRtspUri(e.target.value)}
                required
              />
            </label>
          ) : (
            <>
              <label>
                IP-адрес
                <input
                  value={onvifHost}
                  onChange={(e) => setOnvifHost(e.target.value)}
                  required
                />
              </label>

              <label>
                Порт
                <input
                  type="number"
                  value={onvifPort}
                  onChange={(e) => setOnvifPort(e.target.value)}
                />
              </label>

              <label>
                Имя пользователя
                <input
                  value={onvifUsername}
                  onChange={(e) => setOnvifUsername(e.target.value)}
                />
              </label>

              <label>
                Пароль
                <input
                  type="password"
                  value={onvifPassword}
                  onChange={(e) => setOnvifPassword(e.target.value)}
                />
              </label>

              {camera.onvif?.profile && (
                <div className="modal-readonly">
                  <span className="modal-readonly-label">Текущий профиль</span>
                  <span className="modal-readonly-value">{camera.onvif.profile}</span>
                </div>
              )}

              <p className="modal-hint">
                RTSP-адрес будет получен с камеры автоматически при сохранении.
              </p>
            </>
          )}

          <label>
            Режим записи
            <select
              value={recordingMode}
              onChange={(e) => setRecordingMode(e.target.value as RecordingMode)}
            >
              <option value="continuous">Непрерывная</option>
              <option value="motion">По движению</option>
            </select>
          </label>

          <div className="switch-row">
            <span className="switch-label">
              <span className="switch-title">Детекция движения (ONVIF)</span>
              <span className="switch-hint">
                {sourceType === 'onvif'
                  ? 'События движения и отбор эпизодов в архив'
                  : 'Доступно только для ONVIF-камер'}
              </span>
            </span>
            <label className="switch">
              <input
                type="checkbox"
                checked={motionDetection}
                disabled={sourceType !== 'onvif'}
                onChange={(e) => setMotionDetection(e.target.checked)}
              />
              <span className="switch-slider" />
            </label>
          </div>

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