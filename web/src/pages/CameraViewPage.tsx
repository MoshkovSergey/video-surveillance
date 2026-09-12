import { useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import Hls from 'hls.js';
import { apiFetch, getCameras, getCameraStream } from '../api/client';
import { useToast } from '../components/Toast';
import './CameraViewPage.css';

interface AudioStatus {
  running: boolean;
  mode: string;
  last_error?: string;
  audio_hls_url?: string;
}

// Живой просмотр камеры: видео всегда из основного потока,
// звук — отдельным скрытым аудио-элементом с HLS сайкара G.711 -> AAC.
export default function CameraViewPage() {
  const { id } = useParams<{ id: string }>();
  const notify = useToast();

  const videoRef = useRef<HTMLVideoElement>(null);
  const audioRef = useRef<HTMLAudioElement>(null);
  const hlsVideoRef = useRef<Hls | null>(null);
  const hlsAudioRef = useRef<Hls | null>(null);

  const [soundOn, setSoundOn] = useState(false);
  const [soundBusy, setSoundBusy] = useState(false);

  const { data: cameras } = useQuery({ queryKey: ['cameras'], queryFn: getCameras });
  const camera = (cameras ?? []).find((c) => c.id === id);

  const { data: stream } = useQuery({
    queryKey: ['stream', id],
    queryFn: () => getCameraStream(id as string),
    enabled: !!id,
  });

  // Видео: основной поток камеры, всегда без звука (звук отдельным элементом).
  useEffect(() => {
    const video = videoRef.current;
    const url = stream?.hls_url;
    if (!video || !url) return;

    if (Hls.isSupported()) {
      const hls = new Hls({ enableWorker: true, lowLatencyMode: true });
      hlsVideoRef.current = hls;
      hls.loadSource(url);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        video.play().catch(() => undefined);
      });
      return () => {
        hls.destroy();
        hlsVideoRef.current = null;
      };
    }

    video.src = url;
    video.play().catch(() => undefined);
    return () => {
      video.src = '';
    };
  }, [stream]);

  const attachAudio = (url: string) => {
    const audio = audioRef.current;
    if (!audio) return;

    if (Hls.isSupported()) {
      const hls = new Hls();
      hlsAudioRef.current = hls;
      hls.loadSource(url);
      hls.attachMedia(audio);
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        audio.play().catch(() => undefined);
      });
      return;
    }
    audio.src = url;
    audio.play().catch(() => undefined);
  };

  const detachAudio = () => {
    hlsAudioRef.current?.destroy();
    hlsAudioRef.current = null;
    if (audioRef.current) {
      audioRef.current.pause();
      audioRef.current.src = '';
    }
  };

  const enableSound = async () => {
    if (!id) return;
    setSoundBusy(true);
    try {
      const res = await apiFetch(`/cameras/${id}/audio/start`, { method: 'POST' });
      if (!res.ok) {
        const b = await res.json().catch(() => null);
        throw new Error(b?.error ?? 'не удалось запустить транскодер');
      }

      let status: AudioStatus | null = null;
      for (let i = 0; i < 10; i++) {
        await new Promise((r) => setTimeout(r, 1500));
        const sres = await apiFetch(`/cameras/${id}/audio/status`);
        if (!sres.ok) continue;
        status = (await sres.json()) as AudioStatus;
        if (status.running) break;
      }

      if (!status?.running) {
        throw new Error(status?.last_error || 'транскодер не запустился за 15 секунд');
      }

      const mainUrl = stream?.hls_url ?? '';
      const audioUrl = status.audio_hls_url || mainUrl.replace(`/cam_${id}/`, `/cam_${id}_audio/`);
      attachAudio(audioUrl);
      setSoundOn(true);
      notify('success', 'Звук включён');
    } catch (e) {
      notify('error', (e as Error).message);
    } finally {
      setSoundBusy(false);
    }
  };

  const disableSound = async () => {
    if (!id) return;
    setSoundBusy(true);
    try {
      await apiFetch(`/cameras/${id}/audio/stop`, { method: 'POST' });
    } catch {
      /* не критично: сайкар остановится при выходе со страницы */
    }
    detachAudio();
    setSoundOn(false);
    setSoundBusy(false);
  };

  // При уходе со страницы останавливаем транскодер.
  useEffect(() => {
    return () => {
      detachAudio();
      if (id) {
        apiFetch(`/cameras/${id}/audio/stop`, { method: 'POST' }).catch(() => undefined);
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  return (
    <div className="container">
      <div className="view-header">
        <Link to="/" className="view-back">
          ← К камерам
        </Link>
        <h1 className="view-title">{camera?.name ?? 'Камера'}</h1>
        <button
          className={soundOn ? 'btn-sound-on' : 'btn-probe'}
          disabled={soundBusy}
          onClick={() => (soundOn ? disableSound() : enableSound())}
        >
          {soundBusy ? 'Подождите...' : soundOn ? '🔊 Звук включён' : '🔇 Включить звук'}
        </button>
      </div>

      <div className="view-video-box">
        <video ref={videoRef} controls muted playsInline className="view-video" />
        <audio ref={audioRef} className="view-audio-hidden" />
      </div>

      <p className="view-hint">
        Звук транслируется через транскодер G.711 → AAC; включение занимает несколько секунд.
        При выходе со страницы транскодер останавливается.
      </p>
    </div>
  );
}