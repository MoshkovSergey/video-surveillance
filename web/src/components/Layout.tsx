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
        <span className="app-title">Система видеонаблюдения</span>
        <nav className="app-nav">
          <NavLink to="/" end>
            Камеры
          </NavLink>
          <NavLink to="/archive">Архив</NavLink>
          <NavLink to="/events">События</NavLink>
        </nav>

        <div className="app-header-right">
          {user && (
            <span className="app-user">
              {user.username} · {roleLabels[user.role] ?? user.role}
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