import { NavLink, Outlet } from 'react-router-dom';
import './Layout.css';

export default function Layout() {
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
      </header>
      <main>
        <Outlet />
      </main>
    </div>
  );
}