import { useEffect, useRef } from 'react';
import Hls from 'hls.js';

interface VideoPlayerProps {
  src: string;
}

export default function VideoPlayer({ src }: VideoPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    if (hlsRef.current) {
      hlsRef.current.destroy();
      hlsRef.current = null;
    }

    if (Hls.isSupported()) {
      const hls = new Hls({
        lowLatencyMode: true, // минимизация задержки
      });
      hls.loadSource(src);
      hls.attachMedia(video);

      hlsRef.current = hls;

      return () => {
        hls.destroy();
        hlsRef.current = null;
      };
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      // Нативная поддержка HLS (Safari)
      video.src = src;
    }
  }, [src]);

  return (
    <video
      ref={videoRef}
      className="video-frame"
      controls
      autoPlay
      muted // браузеры блокируют автоплей со звуком
      playsInline
    />
  );
}