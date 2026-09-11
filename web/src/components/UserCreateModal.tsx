import { useState, type FormEvent } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { createUser } from '../api/client';
import { useToast } from './Toast';
import type { UserRole } from '../types/auth';

interface UserCreateModalProps {
  open: boolean;
  onClose: () => void;
}

const roleOptions: { value: UserRole; label: string }[] = [
  { value: 'operator', label: 'Оператор' },
  { value: 'viewer', label: 'Наблюдатель' },
  { value: 'auditor', label: 'Аудитор' },
  { value: 'admin', label: 'Администратор' },
];

// Модальное окно создания пользователя (только для администратора).
export default function UserCreateModal({ open, onClose }: UserCreateModalProps) {
  const queryClient = useQueryClient();
  const notify = useToast();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<UserRole>('viewer');

  const mutation = useMutation({
    mutationFn: () =>
      createUser({
        username: username.trim(),
        password,
        role,
      }),
    onSuccess: (created) => {
      notify('success', `Пользователь ${created.username} создан`);
      queryClient.invalidateQueries({ queryKey: ['users'] });
      setUsername('');
      setPassword('');
      setRole('viewer');
      onClose();
    },
    onError: (error: Error) => {
      notify('error', error.message);
    },
  });

  if (!open) {
    return null;
  }

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    mutation.mutate();
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-card" onClick={(e) => e.stopPropagation()}>
        <h2>Новый пользователь</h2>

        <form className="modal-form" onSubmit={handleSubmit}>
          <label>
            Имя пользователя
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              minLength={3}
              autoComplete="off"
              placeholder="operator1"
              required
            />
          </label>

          <label>
            Пароль
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              minLength={8}
              autoComplete="new-password"
              placeholder="Минимум 8 символов"
              required
            />
          </label>

          <label>
            Роль
            <select value={role} onChange={(e) => setRole(e.target.value as UserRole)}>
              {roleOptions.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </label>

          <div className="modal-actions">
            <button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? 'Создание...' : 'Создать'}
            </button>
            <button
              type="button"
              className="btn-secondary"
              disabled={mutation.isPending}
              onClick={onClose}
            >
              Отмена
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}