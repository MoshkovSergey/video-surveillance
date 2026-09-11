import { useEffect, useRef } from 'react';
import Hls from 'hls.js';

interface VideoPlayerProps {
  src: string;
}

export default function VideoPlayer({ src }: VideoPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  // Отдельный ref для хранения экземпляра hls.js
  const hlsRef = useRef<Hls | null>(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    // Уничтожаем предыдущий экземпляр при смене источника или размонтировании
    if (hlsRef.current) {
      hlsRef.current.destroy();
      hlsRef.current = null;
    }

    if (Hls.isSupported()) {
      const hls = new Hls({
        lowLatencyMode: true, // Пытаемся минимизировать задержку
      });
      hls.loadSource(src);
      hls.attachMedia(video);
      
      // Сохраняем ссылку в ref
      hlsRef.current = hls;

      // Функция очистки при размонтировании компонента или смене src
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
      controls 
      autoPlay 
      muted // Браузеры блокируют автоплей со звуком
      playsInline
      style={{ width: '100%', maxHeight: '70vh', backgroundColor: '#000', borderRadius: '8px' }} 
    />
  );
}