import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { createPlan, deletePlan, getPlans } from '../api/plans';
import { useAuth } from '../auth/AuthContext';
import PlanImage from '../components/PlanImage';
import { useToast } from '../components/Toast';
import './PlansPage.css';

// Список планов объекта; загрузка схемы — только для администратора.
export default function PlansPage() {
  const navigate = useNavigate();
  const notify = useToast();
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const isAdmin = user?.role === 'admin';

  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [file, setFile] = useState<File | null>(null);

  const { data: plans, isLoading } = useQuery({
    queryKey: ['plans'],
    queryFn: getPlans,
  });

  const createMutation = useMutation({
    mutationFn: () => createPlan(name.trim(), file as File),
    onSuccess: () => {
      notify('success', 'План создан');
      setCreating(false);
      setName('');
      setFile(null);
      queryClient.invalidateQueries({ queryKey: ['plans'] });
    },
    onError: (error: Error) => notify('error', error.message),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deletePlan(id),
    onSuccess: () => {
      notify('success', 'План удалён');
      queryClient.invalidateQueries({ queryKey: ['plans'] });
    },
    onError: (error: Error) => notify('error', error.message),
  });

  return (
    <div className="container">
      <div className="plans-header">
        <h1>План объекта</h1>
        {isAdmin && (
          <button className="btn-gradient" onClick={() => setCreating((v) => !v)}>
            {creating ? 'Отмена' : 'Загрузить план'}
          </button>
        )}
      </div>

      {isAdmin && creating && (
        <div className="plans-create">
          <label>
            Наименование
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Этаж 3" />
          </label>
          <label>
            Схема (PNG или JPEG, до 32 МБ)
            <input
              type="file"
              accept="image/png,image/jpeg"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
          </label>
          <button
            className="btn-gradient"
            disabled={!name.trim() || !file || createMutation.isPending}
            onClick={() => createMutation.mutate()}
          >
            {createMutation.isPending ? 'Загрузка...' : 'Создать'}
          </button>
        </div>
      )}

      {isLoading && <div className="loading">Загрузка планов...</div>}

      <div className="plans-grid">
        {(plans ?? []).map((plan) => (
          <div key={plan.id} className="plan-card" onClick={() => navigate(`/plans/${plan.id}`)}>
            <PlanImage id={plan.id} alt={plan.name} className="plan-card-image" />
            <div className="plan-card-footer">
              <div>
                <div className="plan-card-name">{plan.name}</div>
                <div className="plan-card-date">создан {plan.created_at}</div>
              </div>
              {isAdmin && (
                <button
                  className="btn-danger-small"
                  onClick={(e) => {
                    e.stopPropagation();
                    if (window.confirm(`Удалить план «${plan.name}»?`)) {
                      deleteMutation.mutate(plan.id);
                    }
                  }}
                >
                  Удалить
                </button>
              )}
            </div>
          </div>
        ))}
        {!isLoading && (plans ?? []).length === 0 && (
          <div className="error">Планов пока нет{isAdmin ? ' — загрузите первый' : ''}</div>
        )}
      </div>

      <p className="plans-hint">
        <Link to="/">← К камерам</Link>
      </p>
    </div>
  );
}