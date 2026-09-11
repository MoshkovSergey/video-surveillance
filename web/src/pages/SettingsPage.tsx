import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { getSettings, testTelegram, updateSettings } from '../api/client';
import { useAuth } from '../auth/AuthContext';
import { useToast } from '../components/Toast';
import './SettingsPage.css';

const eventTypeOptions: { value: string; label: string }[] = [
  { value: 'motion', label: 'Движение' },
  { value: 'camera_online', label: 'Камера в сети' },
  { value: 'camera_offline', label: 'Потеря связи с камерой' },
  { value: 'fire_alarm', label: 'Пожарная тревога' },
  { value: 'smoke_detection', label: 'Обнаружение дыма' },
  { value: 'manual_alarm', label: 'Ручная тревога' },
  { value: 'recording_error', label: 'Ошибка записи' },
];

// Страница настроек системы (только для администратора).
export default function SettingsPage() {
  const { user } = useAuth();
  const notify = useToast();
  const queryClient = useQueryClient();

  const [enabled, setEnabled] = useState(false);
  const [chatId, setChatId] = useState('');
  const [token, setToken] = useState('');
  const [events, setEvents] = useState<string[]>([]);

  const isAdmin = user?.role === 'admin';

  const { data, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: getSettings,
    enabled: isAdmin,
  });

  useEffect(() => {
    if (!data) return;
    setEnabled(data.telegram_enabled);
    setChatId(data.telegram_chat_id);
    setEvents(data.telegram_events ?? []);
    setToken('');
  }, [data]);

  const saveMutation = useMutation({
    mutationFn: () =>
      updateSettings({
        telegram_enabled: enabled,
        telegram_chat_id: chatId.trim(),
        telegram_bot_token: token.trim() || undefined,
        telegram_events: events,
      }),
    onSuccess: () => {
      notify('success', 'Настройки сохранены');
      queryClient.invalidateQueries({ queryKey: ['settings'] });
    },
    onError: (error: Error) => {
      notify('error', error.message);
    },
  });

  const testMutation = useMutation({
    mutationFn: () =>
      testTelegram({
        telegram_bot_token: token.trim() || undefined,
        telegram_chat_id: chatId.trim() || undefined,
      }),
    onSuccess: () => {
      notify('success', 'Тестовое сообщение отправлено');
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

  const toggleEvent = (value: string) => {
    setEvents((prev) =>
      prev.includes(value) ? prev.filter((v) => v !== value) : [...prev, value],
    );
  };

  return (
    <div className="container">
      <h1>Настройки</h1>

      {isLoading && <div className="loading">Загрузка настроек...</div>}

      <section className="settings-card">
        <div className="settings-card-header">
          <h2>Уведомления: Telegram</h2>
          <span
            className={`toggle-switch ${enabled ? 'toggle-on' : ''}`}
            title={enabled ? 'Отключить уведомления' : 'Включить уведомления'}
            onClick={() => setEnabled((v) => !v)}
          >
            <span className="toggle-knob" />
          </span>
        </div>

        <p className="settings-hint">
          Система отправляет выбранные события журнала в чат Telegram не реже
          чем через 10 секунд после события.{' '}
          {data?.telegram_bot_token_set ? (
            <>
              Текущий токен:{' '}
              <span className="settings-mono">{data.telegram_bot_token_masked}</span>.
            </>
          ) : (
            'Токен бота ещё не задан.'
          )}
        </p>

        <label>
          Токен бота (BotFather)
          <input
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={
              data?.telegram_bot_token_set
                ? 'Оставьте пустым, чтобы не менять'
                : '123456789:AA...'
            }
            autoComplete="new-password"
          />
        </label>

        <label>
          Chat ID / ID группы
          <input
            value={chatId}
            onChange={(e) => setChatId(e.target.value)}
            placeholder="-1001234567890"
          />
        </label>

        <div className="settings-group">
          <span className="settings-group-label">События для отправки</span>
          <div className="settings-events">
            {eventTypeOptions.map((opt) => (
              <label key={opt.value} className="settings-event">
                <input
                  type="checkbox"
                  checked={events.includes(opt.value)}
                  onChange={() => toggleEvent(opt.value)}
                />
                <span>{opt.label}</span>
              </label>
            ))}
          </div>
        </div>

        <div className="settings-actions">
          <button
            className="btn-gradient"
            disabled={saveMutation.isPending}
            onClick={() => saveMutation.mutate()}
          >
            {saveMutation.isPending ? 'Сохранение...' : 'Сохранить'}
          </button>
          <button
            className="btn-probe"
            disabled={testMutation.isPending}
            onClick={() => testMutation.mutate()}
          >
            {testMutation.isPending ? 'Отправка...' : 'Отправить тест'}
          </button>
        </div>
      </section>
    </div>
  );
}