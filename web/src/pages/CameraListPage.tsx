import { useEffect, useRef } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { deleteCamera, getCameras } from '../api/client';
import { useAuth } from '../auth/AuthContext';
import CameraForm from '../components/CameraForm';
import { useToast } from '../components/Toast';
import type { Camera, CameraStatus } from '../types/camera';

const statusLabels: Record<CameraStatus, string> = {
  enabled: 'в сети',
  disabled: 'отключена',
  error: 'нет связи',
};

const getStatusColor = (status: CameraStatus): string => {
  switch (status) {
    case 'enabled':
      return '#28a745';
    case 'disabled':
      return '#6c757d';
    case 'error':
      return '#dc3545';
    default:
      return '#000000';
  }
};

export default function CameraListPage() {
  const queryClient = useQueryClient();
  const notify = useToast();
  const { user } = useAuth();

  const canManage = user?.role === 'admin' || user?.role === 'operator';
  const canDelete = user?.role === 'admin';

  const { data: cameras, isLoading, isError } = useQuery({
    queryKey: ['cameras'],
    queryFn: getCameras,
    refetchInterval: 10_000, // оперативный контроль состояния камер
  });

  // Отслеживаем смены статуса для уведомлений оператора.
  const prevStatuses = useRef<Map<string, CameraStatus> | null>(null);

  useEffect(() => {
    if (!cameras) return;

    const prev = prevStatuses.current;
    const next = new Map<string, CameraStatus>();
    for (const cam of cameras) {
      next.set(cam.id, cam.status);
    }

    if (prev) {
      for (const cam of cameras) {
        const before = prev.get(cam.id);
        if (before === undefined || before === cam.status) continue;

        if (cam.status === 'error') {
          notify('error', `Потеряна связь с камерой «${cam.name}»`);
        } else if (cam.status === 'enabled' && before === 'error') {
          notify('success', `Камера «${cam.name}» снова в сети`);
        }
      }
    }

    prevStatuses.current = next;
  }, [cameras, notify]);

  const deleteMutation = useMutation({
    mutationFn: deleteCamera,
    onSuccess: () => {
      notify('success', 'Камера удалена');
      queryClient.invalidateQueries({ queryKey: ['cameras'] });
    },
    onError: (error: Error) => {
      notify('error', error.message);
    },
  });

  const handleDelete = (id: string, name: string) => {
    if (window.confirm(`Удалить камеру «${name}»? Действие необратимо.`)) {
      deleteMutation.mutate(id);
    }
  };

  if (isLoading) return <div className="loading">Загрузка камер...</div>;
  if (isError)
    return (
      <div className="error">
        Ошибка загрузки камер. Убедитесь, что бэкенд запущен.
      </div>
    );

  return (
    <div className="container">
      <h1>Камеры видеонаблюдения</h1>

      {canManage && <CameraForm />}

      <table className="camera-table">
        <thead>
          <tr>
            <th>Имя камеры</th>
            <th>Расположение</th>
            <th>Статус</th>
            <th>Действия</th>
          </tr>
        </thead>
        <tbody>
          {cameras?.map((cam: Camera) => (
            <tr key={cam.id}>
              <td>{cam.name}</td>
              <td>{cam.location || '—'}</td>
              <td>
                <span
                  className="status-badge"
                  style={{ backgroundColor: getStatusColor(cam.status) }}
                >
                  {statusLabels[cam.status] ?? cam.status}
                </span>
              </td>
              <td>
                <Link to={`/cameras/${cam.id}/view`} className="btn-view">
                  Смотреть
                </Link>
                {canDelete && (
                  <button
                    className="btn-delete"
                    disabled={deleteMutation.isPending}
                    onClick={() => handleDelete(cam.id, cam.name)}
                  >
                    Удалить
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}