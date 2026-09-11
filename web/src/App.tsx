import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ToastProvider } from './components/Toast';
import Layout from './components/Layout';
import CameraListPage from './pages/CameraListPage';
import CameraViewPage from './pages/CameraViewPage';
import ArchivePage from './pages/ArchivePage';

const queryClient = new QueryClient();

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <BrowserRouter>
          <Routes>
            <Route element={<Layout />}>
              <Route path="/" element={<CameraListPage />} />
              <Route path="/cameras/:id/view" element={<CameraViewPage />} />
              <Route path="/archive" element={<ArchivePage />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </ToastProvider>
    </QueryClientProvider>
  );
}

export default App;