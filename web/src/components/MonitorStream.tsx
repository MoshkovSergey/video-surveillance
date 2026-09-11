import { useQuery } from '@tanstack/react-query';
import { getCameraStream } from '../api/client';
import VideoPlayer from './VideoPlayer';

interface MonitorStreamProps {
  cameraId: string;
}

// Поток одной ячейки монитора: адрес HLS берётся из API и кэшируется react-query.
export default function MonitorStream({ cameraId }: MonitorStreamProps) {
  const { data, isError, isLoading } = useQuery({
    queryKey: ['stream', cameraId],
    queryFn: () => getCameraStream(cameraId),
    staleTime: 60_000,
  });

  if (isLoading) {
    return <div className="cell-loading">Подключение...</div>;
  }

  if (isError || !data) {
    return <div className="cell-loading cell-error">Нет потока</div>;
  }

  return <VideoPlayer src={data.hls_url} />;
}