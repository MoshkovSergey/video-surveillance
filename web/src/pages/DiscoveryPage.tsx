import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { createCamera, probeOnvif, scanNetwork } from '../api/client';
import { useAuth } from '../auth/AuthContext';
import { useToast } from '../components/Toast';
import type { DiscoveredDevice } from '../types/discovery';
import type { ONVIFProfile } from '../types/camera';
import './DiscoveryPage.css';

interface AddState {
  device: DiscoveredDevice;
  username: string;
  password: string;
  name: string;
  location: string;
  profiles: ONVIFProfile[];
  profile: ONVIFProfile | null;
}

function formatSeconds(ms: number): string {
  return `${(ms / 1000).toFixed(1)} с`;
}

// Раздел поиска камер в локальной сети (только для администратора).
export default function DiscoveryPage() {
  const { user } = useAuth();
  const notify = useToast();
  const queryClient = useQueryClient();

  const [devices, setDevices] = useState<DiscoveredDevice[] | null>(null);
  const [lastDurationMs, setLastDurationMs] = useState<number | null>(null);
  const [add, setAdd] = useState<AddState | null>(null);

  const isAdmin = user?.role === 'admin';

  const scanMutation = useMutation({
    mutationFn: scanNetwork,
    onSuccess: (result) => {
      setDevices(result.devices);
      setLastDurationMs(result.duration_ms);

      const spent = formatSeconds(result.duration_ms);
      if (result.devices.length === 0) {
        notify('info', `ONVIF-устройств не найдено (скан: ${spent})`);
      } else {
        notify('success', `Найдено устройств: ${result.devices.length} (скан: ${spent})`);
      }
    },
    onError: (error: Error) => {
      notify('error', error.message);
    },
  });

  const probeMutation = useMutation({
    mutationFn: (state: AddState) =>
      probeOnvif({
        host: state.device.host,
        port: state.device.port,
        username: state.username,
        password: state.password,
      }),
    onSuccess: (profiles) => {
      setAdd((prev) => (prev ? { ...prev, profiles, profile: null } : prev));
      if (profiles.length === 0) {
        notify('error', 'Профили не найдены');
      } else {
        notify('success', `Найдено профилей: ${profiles.length}`);
      }
    },
    onError: (error: Error) => {
      notify('error', error.message);
      setAdd((prev) => (prev ? { ...prev, profiles: [] } : prev));
    },
  });

  const createMutation = useMutation({
    mutationFn: (state: AddState) => {
      if (!state.profile) {
        throw new Error('Выберите профиль камеры');
      }
      return createCamera({
        name: state.name.trim() || state.device.name,
        location: state.location.trim(),
        source_type: 'onvif',
        onvif: {
          host: state.device.host,
          port: state.device.port,
          username: state.username,
          password: state.password,
          profile: state.profile.token,
        },
      });
    },
    onSuccess: () => {
      notify('success', 'Камера добавлена');
      queryClient.invalidateQueries({ queryKey: ['cameras'] });
      setDevices((prev) =>
        prev?.map((d) =>
          d.host === add?.device.host ? { ...d, added: true } : d,
        ) ?? prev,
      );
      setAdd(null);
    },
    onError: (error: Error) => {
      notify('error', error.message);
    },
  });

  if (!isAdmin) {
    return (
      <div className="container">
        <div className="error">Раздел доступен только администратору</div>
      </div>
    );
  }

  const openAdd = (device: DiscoveredDevice) => {
    setAdd({
      device,
      username: 'admin',
      password: '',
      name: device.name,
      location: '',
      profiles: [],
      profile: null,
    });
  };

  return (
    <div className="container">
      <h1>Поиск камер в сети</h1>

      <div className="disc-panel">
        <p className="disc-desc">
          Сканирование выполняется по протоколу ONVIF WS-Discovery в локальном
          сегменте сети и длится до 20 секунд либо завершается раньше, когда
          все ответившие устройства обнаружены. Устройства без поддержки ONVIF
          добавьте вручную через RTSP.
        </p>

        <button
          className="btn-gradient disc-scan-btn"
          disabled={scanMutation.isPending}
          onClick={() => scanMutation.mutate()}
        >
          {scanMutation.isPending ? (
            <>
              <span className="disc-radar" />
              Сканирование... (до 20 с)
            </>
          ) : (
            'Сканировать сеть'
          )}
        </button>

        {lastDurationMs !== null && !scanMutation.isPending && (
          <p className="disc-spent">
            Время сканирования:{' '}
            <span className="disc-spent-value">{formatSeconds(lastDurationMs)}</span>
            {devices !== null && (
              <>
                {' '}· найдено устройств:{' '}
                <span className="disc-spent-value">{devices.length}</span>
              </>
            )}
          </p>
        )}
      </div>

      {devices !== null && devices.length === 0 && !scanMutation.isPending && (
        <div className="loading">Устройства не найдены</div>
      )}

      {devices !== null && devices.length > 0 && (
        <table className="camera-table">
          <thead>
            <tr>
              <th>Имя</th>
              <th>Производитель</th>
              <th>Модель</th>
              <th>Адрес</th>
              <th>Статус</th>
              <th>Действия</th>
            </tr>
          </thead>
          <tbody>
            {devices.map((d) => (
              <tr key={`${d.host}:${d.port}`}>
                <td>{d.name}</td>
                <td>{d.manufacturer || '—'}</td>
                <td>{d.model || '—'}</td>
                <td className="disc-addr">
                  {d.host}:{d.port}
                </td>
                <td>
                  {d.added ? (
                    <span className="disc-badge disc-badge-added">в системе</span>
                  ) : (
                    <span className="disc-badge disc-badge-new">найдена</span>
                  )}
                </td>
                <td>
                  {d.added ? (
                    <span className="disc-muted">добавлена</span>
                  ) : (
                    <button className="btn-small" onClick={() => openAdd(d)}>
                      Добавить
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {add && (
        <div className="modal-overlay" onClick={() => setAdd(null)}>
          <div className="modal-card" onClick={(e) => e.stopPropagation()}>
            <h2>Добавить найденную камеру</h2>

            <div className="modal-form">
              <div className="modal-readonly">
                <span className="modal-readonly-label">Адрес устройства</span>
                <span className="modal-readonly-value">
                  {add.device.host}:{add.device.port}
                </span>
              </div>

              <label>
                Название
                <input
                  value={add.name}
                  onChange={(e) => setAdd({ ...add, name: e.target.value })}
                />
              </label>

              <label>
                Расположение
                <input
                  value={add.location}
                  onChange={(e) => setAdd({ ...add, location: e.target.value })}
                />
              </label>

              <label>
                Пользователь ONVIF
                <input
                  value={add.username}
                  onChange={(e) => setAdd({ ...add, username: e.target.value })}
                />
              </label>

              <label>
                Пароль ONVIF
                <input
                  type="password"
                  value={add.password}
                  onChange={(e) => setAdd({ ...add, password: e.target.value })}
                />
              </label>

              <button
                type="button"
                className="btn-probe"
                disabled={probeMutation.isPending}
                onClick={() => probeMutation.mutate(add)}
              >
                {probeMutation.isPending ? 'Опрос...' : 'Опросить профили'}
              </button>

              {add.profiles.length > 0 && (
                <div className="profiles-list">
                  <p className="profiles-title">Доступные профили:</p>
                  {add.profiles.map((p) => (
                    <label key={p.token} className="profile-option">
                      <input
                        type="radio"
                        name="disc-profile"
                        checked={add.profile?.token === p.token}
                        onChange={() => setAdd({ ...add, profile: p })}
                      />
                      <div className="profile-info">
                        <span className="profile-name">{p.name || p.token}</span>
                        <span className="profile-uri">{p.stream_uri}</span>
                      </div>
                    </label>
                  ))}
                </div>
              )}

              <div className="modal-actions">
                <button
                  type="button"
                  disabled={createMutation.isPending}
                  onClick={() => createMutation.mutate(add)}
                >
                  {createMutation.isPending ? 'Добавление...' : 'Добавить в систему'}
                </button>
                <button
                  type="button"
                  className="btn-secondary"
                  onClick={() => setAdd(null)}
                >
                  Отмена
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}