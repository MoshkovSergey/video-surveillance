import { useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import Hls from 'hls.js';
import {
  getCameras,
  getCameraStream,
  startCameraAudio,
  stopCameraAudio,
} from '../api/client';
import { useToast } from '../components/Toast';
import './CameraViewPage.css';

// Увеличенный экран камеры с возможностью включить живой звук.
export default function CameraViewPage() {
  const { id } = useParams<{ id: string }>();
  const notify = useToast();

  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const soundOnRef = useRef(false);

  const [soundOn, setSoundOn] = useState(false);
  const [switching, setSwitching] = useState(false);

  const { data: cameras } = useQuery({
    queryKey: ['cameras'],
    queryFn: getCameras,
  });

  const { data: stream, isLoading } = useQuery({
    queryKey: ['stream', id],
    queryFn: () => getCameraStream(id as string),
    enabled: !!id,
  });

  const camera = cameras?.find((c) => c.id === id);

  // Подключение плеера к HLS-потоку.
  const attach = (url: string, muted: boolean) => {
    const video = videoRef.current;
    if (!video) return;

    if (hlsRef.current) {
      hlsRef.current.destroy();
      hlsRef.current = null;
    }

    if (Hls.isSupported()) {
      const hls = new Hls({ enableWorker: true, lowLatencyMode: true });
      hls.loadSource(url);
      hls.attachMedia(video);
      hlsRef.current = hls;
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = url;
    }

    video.muted = muted;
    video.play().catch(() => {});
  };

  useEffect(() => {
    if (stream) {
      attach(stream.hls_url, true);
    }
    return () => {
      if (hlsRef.current) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stream]);

  // При уходе со страницы останавливаем звуковой сайкар.
  useEffect(() => {
    return () => {
      if (soundOnRef.current && id) {
        stopCameraAudio(id).catch(() => {});
      }
    };
  }, [id]);

  const audioHlsUrl = (base: string) =>
    base.replace(`/cam_${id}/`, `/cam_${id}_audio/`);

  const toggleSound = async () => {
    if (!id || !stream || switching) return;

    setSwitching(true);
    try {
      if (!soundOn) {
        await startCameraAudio(id);
        // Даём транскодеру опубликовать поток и накопить сегменты HLS.
        await new Promise((r) => setTimeout(r, 2500));
        attach(audioHlsUrl(stream.hls_url), false);
        soundOnRef.current = true;
        setSoundOn(true);
        notify('success', 'Звук включён');
      } else {
        await stopCameraAudio(id);
        attach(stream.hls_url, true);
        soundOnRef.current = false;
        setSoundOn(false);
        notify('info', 'Звук выключен');
      }
    } catch (error) {
      notify('error', (error as Error).message);
    } finally {
      setSwitching(false);
    }
  };

  return (
    <div className="container">
      <div className="view-header">
        <Link to="/" className="view-back">
          ← К камерам
        </Link>
        <h1 className="view-title">{camera?.name ?? 'Камера'}</h1>
        <button
          className={`btn-sound ${soundOn ? 'btn-sound-on' : ''}`}
          onClick={toggleSound}
          disabled={switching || !stream}
          title={soundOn ? 'Выключить звук' : 'Включить живой звук'}
        >
          {switching ? '…' : soundOn ? '🔊 Звук включён' : '🔇 Включить звук'}
        </button>
      </div>

      {isLoading && <div className="loading">Подключение к потоку...</div>}

      <div className="view-player-wrap">
        <video
          ref={videoRef}
          className="view-player"
          controls
          autoPlay
          muted
          playsInline
        />
      </div>

      <p className="view-hint">
        Звук транслируется через транскодер G.711 → AAC; включение занимает
        несколько секунд. При выходе со страницы транскодер останавливается.
      </p>
    </div>
  );
}