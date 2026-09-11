import { useState, type FormEvent } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { createCamera, probeOnvif } from '../api/client';
import { useToast } from './Toast';
import type { CameraSourceType, ONVIFParams, ONVIFProfile } from '../types/camera';
import './CameraForm.css';

// Форма создания камеры с поддержкой RTSP и ONVIF.
export default function CameraForm() {
  const queryClient = useQueryClient();
  const notify = useToast();

  const [sourceType, setSourceType] = useState<CameraSourceType>('rtsp');

  // RTSP поля
  const [name, setName] = useState('');
  const [rtspUri, setRtspUri] = useState('');
  const [location, setLocation] = useState('');

  // ONVIF поля
  const [onvifHost, setOnvifHost] = useState('');
  const [onvifPort, setOnvifPort] = useState('80');
  const [onvifUsername, setOnvifUsername] = useState('admin');
  const [onvifPassword, setOnvifPassword] = useState('');

  // Профили ONVIF
  const [profiles, setProfiles] = useState<ONVIFProfile[]>([]);
  const [selectedProfile, setSelectedProfile] = useState<ONVIFProfile | null>(null);
  const [probing, setProbing] = useState(false);

  const mutation = useMutation({
    mutationFn: () => {
      const payload: Parameters<typeof createCamera>[0] = {
        name: name.trim(),
        location: location.trim(),
        source_type: sourceType,
      };

      if (sourceType === 'rtsp') {
        payload.rtsp_uri = rtspUri.trim();
      } else {
        if (!selectedProfile) {
          throw new Error('Выберите профиль камеры');
        }
        payload.onvif = {
          host: onvifHost.trim(),
          port: parseInt(onvifPort, 10),
          username: onvifUsername.trim(),
          password: onvifPassword,
          profile: selectedProfile.token,
        };
      }

      return createCamera(payload);
    },
    onSuccess: () => {
      notify('success', 'Камера добавлена');
      queryClient.invalidateQueries({ queryKey: ['cameras'] });
      setName('');
      setRtspUri('');
      setLocation('');
      setOnvifHost('');
      setOnvifPort('80');
      setOnvifUsername('admin');
      setOnvifPassword('');
      setProfiles([]);
      setSelectedProfile(null);
    },
    onError: (error: Error) => {
      notify('error', error.message);
    },
  });

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    mutation.mutate();
  };

  const handleProbe = async () => {
    if (!onvifHost.trim()) {
      notify('error', 'Укажите IP-адрес камеры');
      return;
    }

    setProbing(true);
    try {
      const params: ONVIFParams = {
        host: onvifHost.trim(),
        port: parseInt(onvifPort, 10) || 80,
        username: onvifUsername.trim(),
        password: onvifPassword,
      };
      const result = await probeOnvif(params);
      setProfiles(result);
      setSelectedProfile(null);

      if (result.length === 0) {
        notify('error', 'Профили не найдены');
      } else {
        notify('success', `Найдено профилей: ${result.length}`);
      }
    } catch (error) {
      notify('error', (error as Error).message);
      setProfiles([]);
      setSelectedProfile(null);
    } finally {
      setProbing(false);
    }
  };

  return (
    <form className="camera-form" onSubmit={handleSubmit}>
      <h2>Добавить камеру</h2>

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
          placeholder="Камера 1"
        />
      </label>

      <label>
        Расположение
        <input
          value={location}
          onChange={(e) => setLocation(e.target.value)}
          placeholder="Главный вход"
        />
      </label>

      {sourceType === 'rtsp' ? (
        <label>
          RTSP URI
          <input
            value={rtspUri}
            onChange={(e) => setRtspUri(e.target.value)}
            required
            placeholder="rtsp://admin:password@192.168.1.100:554/stream"
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
              placeholder="192.168.1.100"
            />
          </label>

          <label>
            Порт
            <input
              type="number"
              value={onvifPort}
              onChange={(e) => setOnvifPort(e.target.value)}
              placeholder="80"
            />
          </label>

          <label>
            Имя пользователя
            <input
              value={onvifUsername}
              onChange={(e) => setOnvifUsername(e.target.value)}
              placeholder="admin"
            />
          </label>

          <label>
            Пароль
            <input
              type="password"
              value={onvifPassword}
              onChange={(e) => setOnvifPassword(e.target.value)}
              placeholder="••••••••"
            />
          </label>

          <button
            type="button"
            className="btn-probe"
            onClick={handleProbe}
            disabled={probing}
          >
            {probing ? 'Опрос...' : 'Опросить профили'}
          </button>

          {profiles.length > 0 && (
            <div className="profiles-list">
              <p className="profiles-title">Доступные профили:</p>
              {profiles.map((p) => (
                <label key={p.token} className="profile-option">
                  <input
                    type="radio"
                    name="onvif-profile"
                    checked={selectedProfile?.token === p.token}
                    onChange={() => setSelectedProfile(p)}
                  />
                  <div className="profile-info">
                    <span className="profile-name">{p.name || p.token}</span>
                    <span className="profile-uri">{p.stream_uri}</span>
                  </div>
                </label>
              ))}
            </div>
          )}
        </>
      )}

      <button type="submit" disabled={mutation.isPending}>
        {mutation.isPending ? 'Создание...' : 'Создать'}
      </button>
    </form>
  );
}