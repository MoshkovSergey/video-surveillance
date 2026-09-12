import { useEffect, useState } from 'react';
import { getPlanImageBlob } from '../api/plans';

interface Props {
  id: string;
  alt: string;
  className?: string;
  draggable?: boolean;
}

// PlanImage загружает схему плана через авторизованный запрос
// (тег img не передаёт заголовок Authorization) и отображает blob-URL.
export default function PlanImage({ id, alt, className, draggable }: Props) {
  const [url, setUrl] = useState<string | null>(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    let alive = true;
    let created: string | null = null;

    getPlanImageBlob(id)
      .then((blob) => {
        if (!alive) return;
        created = URL.createObjectURL(blob);
        setUrl(created);
      })
      .catch(() => {
        if (alive) setError(true);
      });

    return () => {
      alive = false;
      if (created) URL.revokeObjectURL(created);
    };
  }, [id]);

  if (error) {
    return <div className="plan-image-error">Схема недоступна</div>;
  }
  if (!url) {
    return <div className={`plan-image-loading ${className ?? ''}`} />;
  }
  return <img src={url} alt={alt} className={className} draggable={draggable} />;
}