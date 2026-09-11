import { NavLink, Outlet } from 'react-router-dom';
import { useAuth } from '../auth/AuthContext';
import type { UserRole } from '../types/auth';
import './Layout.css';

const roleLabels: Record<UserRole, string> = {
  admin: 'администратор',
  operator: 'оператор',
  viewer: 'наблюдатель',
  auditor: 'аудитор',
};

export default function Layout() {
  const { user, logout } = useAuth();

  return (
    <div className="app-shell">
      <header className="app-header">
        <div className="app-logo">
          <span className="logo-mark">VS</span>
          <span className="app-title">Система видеонаблюдения</span>
        </div>

        <nav className="app-nav">
          <NavLink to="/" end>
            Камеры
          </NavLink>
          <NavLink to="/monitor">
            Монитор
          </NavLink>
          <NavLink to="/archive">
            Архив
          </NavLink>
          <NavLink to="/events">
            События
          </NavLink>
          {user?.role === 'admin' && (
            <>
              <NavLink to="/discovery">
                Поиск камер
              </NavLink>
              <NavLink to="/users">
                Пользователи
              </NavLink>
              <NavLink to="/settings">
                Настройки
              </NavLink>
            </>
          )}
        </nav>

        <div className="app-header-right">
          {user && (
            <span className="app-user">
              <span className="user-name">{user.username}</span>
              <span className="user-role">{roleLabels[user.role] ?? user.role}</span>
            </span>
          )}
          {user && (
            <button className="btn-logout" onClick={logout}>
              Выйти
            </button>
          )}
        </div>
      </header>
      <main>
        <Outlet />
      </main>
    </div>
  );
}