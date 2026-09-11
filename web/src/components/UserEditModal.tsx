import { useEffect, useState, type FormEvent } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { updateUser } from '../api/client';
import { useToast } from './Toast';
import type { UserDTO, UserRole } from '../types/auth';

interface UserEditModalProps {
  user: UserDTO | null;
  onClose: () => void;
}

const roleOptions: { value: UserRole; label: string }[] = [
  { value: 'admin', label: 'Администратор' },
  { value: 'operator', label: 'Оператор' },
  { value: 'viewer', label: 'Наблюдатель' },
  { value: 'auditor', label: 'Аудитор' },
];

// Модальное окно редактирования пользователя (только для администратора).
export default function UserEditModal({ user, onClose }: UserEditModalProps) {
  const queryClient = useQueryClient();
  const notify = useToast();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<UserRole>('viewer');
  const [isActive, setIsActive] = useState(true);

  useEffect(() => {
    if (!user) return;
    setUsername(user.username);
    setPassword('');
    setRole(user.role);
    setIsActive(user.is_active);
  }, [user]);

  const mutation = useMutation({
    mutationFn: () => {
      if (!user) {
        throw new Error('user is not selected');
      }
      const payload: Parameters<typeof updateUser>[1] = {
        username: username.trim(),
        role,
        is_active: isActive,
      };
      if (password.trim() !== '') {
        payload.password = password;
      }
      return updateUser(user.id, payload);
    },
    onSuccess: () => {
      notify('success', 'Пользователь обновлён');
      queryClient.invalidateQueries({ queryKey: ['users'] });
      onClose();
    },
    onError: (error: Error) => {
      notify('error', error.message);
    },
  });

  if (!user) {
    return null;
  }

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    mutation.mutate();
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-card" onClick={(e) => e.stopPropagation()}>
        <h2>Изменить пользователя</h2>

        <form className="modal-form" onSubmit={handleSubmit}>
          <label>
            Имя пользователя
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              minLength={3}
              required
            />
          </label>

          <label>
            Новый пароль
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              minLength={8}
              placeholder="Оставьте пустым, чтобы не менять"
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

          <label className="toggle-label">
            <span className="toggle-text">
              <span className="toggle-title">Учётная запись активна</span>
              <span className="toggle-hint">Отключённые пользователи не могут войти</span>
            </span>
            <span
              className={`toggle-switch ${isActive ? 'toggle-on' : ''}`}
              onClick={() => setIsActive((v) => !v)}
            >
              <span className="toggle-knob" />
            </span>
          </label>

          <div className="modal-actions">
            <button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? 'Сохранение...' : 'Сохранить'}
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