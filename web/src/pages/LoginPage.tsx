import { useState, type FormEvent } from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from '../auth/AuthContext';
import { useToast } from '../components/Toast';
import './LoginPage.css';

export default function LoginPage() {
  const { user, login } = useAuth();
  const notify = useToast();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);

  if (user) {
    return <Navigate to="/" replace />;
  }

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setSubmitting(true);
    try {
      await login(username.trim(), password);
    } catch (error) {
      notify('error', (error as Error).message || 'Ошибка входа');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={handleSubmit}>
        <h1>Система видеонаблюдения</h1>
        <p className="login-subtitle">Вход в систему</p>

        <label>
          Имя пользователя
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            required
          />
        </label>

        <label>
          Пароль
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>

        <button type="submit" disabled={submitting}>
          {submitting ? 'Вход...' : 'Войти'}
        </button>
      </form>
    </div>
  );
}