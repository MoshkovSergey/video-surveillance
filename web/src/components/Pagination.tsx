import './Pagination.css';

interface Props {
  current: number;
  total: number;
  onPage: (page: number) => void;
}

// Компонент пагинации: кнопки «Назад», номера страниц, «Вперёд».
export default function Pagination({ current, total, onPage }: Props) {
  if (total <= 1) return null;

  const pages: number[] = [];
  const start = Math.max(1, current - 2);
  const end = Math.min(total, current + 2);

  if (start > 1) {
    pages.push(1);
    if (start > 2) pages.push(-1); // многоточие
  }

  for (let i = start; i <= end; i++) {
    pages.push(i);
  }

  if (end < total) {
    if (end < total - 1) pages.push(-1);
    pages.push(total);
  }

  return (
    <div className="pagination">
      <button
        className="pagination-btn"
        disabled={current === 1}
        onClick={() => onPage(current - 1)}
      >
        ← Назад
      </button>
      {pages.map((p, idx) =>
        p === -1 ? (
          <span key={`ellipsis-${idx}`} className="pagination-ellipsis">
            …
          </span>
        ) : (
          <button
            key={p}
            className={`pagination-btn ${p === current ? 'pagination-btn-active' : ''}`}
            onClick={() => onPage(p)}
          >
            {p}
          </button>
        ),
      )}
      <button
        className="pagination-btn"
        disabled={current === total}
        onClick={() => onPage(current + 1)}
      >
        Вперёд →
      </button>
    </div>
  );
}