import { useParams, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { getCameraStream } from '../api/client';
import VideoPlayer from '../components/VideoPlayer';

export default function CameraViewPage() {
  const { id } = useParams<{ id: string }>();

  const { data: stream, isLoading, isError } = useQuery({
    queryKey: ['stream', id],
    queryFn: () => getCameraStream(id!),
    enabled: !!id, // Запрос выполняется только если есть ID
  });

  if (isLoading) return <div className="loading">Подключение к видеопотоку...</div>;
  if (isError) return <div className="error">Ошибка получения данных потока</div>;

  return (
    <div className="container">
      <div className="view-header">
        <h2>Просмотр камеры</h2>
        <Link to="/" className="btn-back">← Назад к списку</Link>
      </div>

      <div className="video-container">
        {stream && <VideoPlayer src={stream.hls_url} />}
      </div>

      <div className="stream-info">
        <p><strong>ID камеры:</strong> {stream?.camera_id}</p>
        <p><strong>RTSP (для записи/архива):</strong> {stream?.rtsp_url}</p>
        <p><strong>WebRTC (низкая задержка):</strong> {stream?.webrtc_url}</p>
      </div>
    </div>
  );
}