import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { getCameras } from '../api/client';
import type { CameraStatus } from '../types/camera';

// Вспомогательная функция для цветового индикатора статуса
const getStatusColor = (status: CameraStatus) => {
  switch (status) {
    case 'enabled': return '#28a745'; // Зеленый
    case 'disabled': return '#6c757d'; // Серый
    case 'error': return '#dc3545'; // Красный
    default: return '#000';
  }
};

export default function CameraListPage() {
  const { data: cameras, isLoading, isError } = useQuery({
    queryKey: ['cameras'],
    queryFn: getCameras,
  });

  if (isLoading) return <div className="loading">Загрузка камер...</div>;
  if (isError) return <div className="error">Ошибка загрузки камер. Убедитесь, что бэкенд запущен.</div>;

  return (
    <div className="container">
      <h1>Камеры видеонаблюдения</h1>
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
          {cameras?.map((cam) => (
            <tr key={cam.id}>
              <td>{cam.name}</td>
              <td>{cam.location || '—'}</td>
              <td>
                <span 
                  className="status-badge" 
                  style={{ backgroundColor: getStatusColor(cam.status) }}
                >
                  {cam.status}
                </span>
              </td>
              <td>
                <Link to={`/cameras/${cam.id}/view`} className="btn-view">
                  Смотреть
                </Link>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}