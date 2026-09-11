import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { deleteUser, getUsers } from '../api/client';
import { useAuth } from '../auth/AuthContext';
import UserCreateModal from '../components/UserCreateModal';
import UserEditModal from '../components/UserEditModal';
import { useToast } from '../components/Toast';
import type { UserDTO, UserRole } from '../types/auth';
import './UsersPage.css';

const roleLabels: Record<UserRole, string> = {
  admin: 'администратор',
  operator: 'оператор',
  viewer: 'наблюдатель',
  auditor: 'аудитор',
};

export default function UsersPage() {
  const { user } = useAuth();
  const notify = useToast();
  const queryClient = useQueryClient();

  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<UserDTO | null>(null);

  const isAdmin = user?.role === 'admin';

  const { data: users, isLoading, isError } = useQuery({
    queryKey: ['users'],
    queryFn: getUsers,
    enabled: isAdmin,
  });

  const deleteMutation = useMutation({
    mutationFn: deleteUser,
    onSuccess: () => {
      notify('success', 'Пользователь удалён');
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (error: Error) => {
      notify('error', error.message);
    },
  });

  if (!isAdmin) {
    return (
      <div className="container">
        <div className="error">Раздел доступен только администратору</div>
      </div>
    );
  }

  const handleDelete = (target: UserDTO) => {
    if (target.role === 'admin') {
      notify('error', 'Администратора нельзя удалить — только изменить');
      return;
    }
    if (window.confirm(`Удалить пользователя «${target.username}»?`)) {
      deleteMutation.mutate(target.id);
    }
  };

  return (
    <div className="container">
      <h1>Пользователи</h1>

      <div className="users-toolbar">
        <button className="btn-gradient" onClick={() => setCreateOpen(true)}>
          Добавить пользователя
        </button>
      </div>

      {isLoading && <div className="loading">Загрузка пользователей...</div>}
      {isError && <div className="error">Ошибка загрузки списка пользователей</div>}

      {users && users.length > 0 && (
        <table className="camera-table">
          <thead>
            <tr>
              <th>Имя пользователя</th>
              <th>Роль</th>
              <th>Состояние</th>
              <th>Действия</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id}>
                <td>{u.username}</td>
                <td>
                  <span className={`role-badge role-${u.role}`}>
                    {roleLabels[u.role] ?? u.role}
                  </span>
                </td>
                <td>
                  {u.is_active ? (
                    <span className="user-state user-active">активен</span>
                  ) : (
                    <span className="user-state user-disabled">отключён</span>
                  )}
                </td>
                <td>
                  <button className="btn-edit" onClick={() => setEditing(u)}>
                    Изменить
                  </button>
                  <button
                    className="btn-delete"
                    disabled={u.role === 'admin' || deleteMutation.isPending}
                    title={
                      u.role === 'admin'
                        ? 'Администратора нельзя удалить — только изменить'
                        : 'Удалить пользователя'
                    }
                    onClick={() => handleDelete(u)}
                  >
                    Удалить
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {users && users.length === 0 && !isLoading && (
        <div className="loading">Пользователей не найдено</div>
      )}

      <UserCreateModal open={createOpen} onClose={() => setCreateOpen(false)} />
      <UserEditModal user={editing} onClose={() => setEditing(null)} />
    </div>
  );
}