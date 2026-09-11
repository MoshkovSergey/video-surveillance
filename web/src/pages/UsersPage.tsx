import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { getUsers } from '../api/client';
import { useAuth } from '../auth/AuthContext';
import UserCreateModal from '../components/UserCreateModal';
import type { UserRole } from '../types/auth';
import './UsersPage.css';

const roleLabels: Record<UserRole, string> = {
  admin: 'администратор',
  operator: 'оператор',
  viewer: 'наблюдатель',
  auditor: 'аудитор',
};

export default function UsersPage() {
  const { user } = useAuth();
  const [createOpen, setCreateOpen] = useState(false);

  const isAdmin = user?.role === 'admin';

  const { data: users, isLoading, isError } = useQuery({
    queryKey: ['users'],
    queryFn: getUsers,
    enabled: isAdmin, // не-админам запрос не выполняется
  });

  // Двойная защита: даже при прямом заходе по URL страница закрыта.
  if (!isAdmin) {
    return (
      <div className="container">
        <div className="error">Раздел доступен только администратору</div>
      </div>
    );
  }

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
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {users && users.length === 0 && !isLoading && (
        <div className="loading">Пользователей не найдено</div>
      )}

      <UserCreateModal open={createOpen} onClose={() => setCreateOpen(false)} />
    </div>
  );
}