import { useState, type FormEvent } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { createCamera } from '../api/client';
import { useToast } from './Toast';

export default function CameraForm() {
  const [name, setName] = useState('');
  const [rtspUri, setRtspUri] = useState('');
  const [location, setLocation] = useState('');

  const queryClient = useQueryClient();
  const notify = useToast();

  const mutation = useMutation({
    mutationFn: createCamera,
    onSuccess: () => {
      notify('success', 'Камера успешно создана');
      setName('');
      setRtspUri('');
      setLocation('');
      queryClient.invalidateQueries({ queryKey: ['cameras'] });
    },
    onError: (error: Error) => {
      notify('error', error.message);
    },
  });

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    mutation.mutate({
      name: name.trim(),
      rtsp_uri: rtspUri.trim(),
      location: location.trim() || undefined,
    });
  };

  return (
    <form className="camera-form" onSubmit={handleSubmit}>
      <h2>Добавить камеру</h2>

      <label>
        Название
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Камера на входе"
          required
        />
      </label>

      <label>
        RTSP URI
        <input
          value={rtspUri}
          onChange={(e) => setRtspUri(e.target.value)}
          placeholder="rtsp://user:password@192.168.1.100:554/stream"
          required
        />
      </label>

      <label>
        Расположение
        <input
          value={location}
          onChange={(e) => setLocation(e.target.value)}
          placeholder="Главный вход"
        />
      </label>

      <button type="submit" disabled={mutation.isPending}>
        {mutation.isPending ? 'Сохранение...' : 'Создать камеру'}
      </button>
    </form>
  );
}