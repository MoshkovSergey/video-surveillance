import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ToastProvider } from './components/Toast';
import RequireAuth from './components/RequireAuth';
import Layout from './components/Layout';
import { AuthProvider } from './auth/AuthContext';
import LoginPage from './pages/LoginPage';
import CameraListPage from './pages/CameraListPage';
import CameraViewPage from './pages/CameraViewPage';
import ArchivePage from './pages/ArchivePage';
import EventsPage from './pages/EventsPage';

const queryClient = new QueryClient();

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <BrowserRouter>
          <AuthProvider>
            <Routes>
              <Route path="/login" element={<LoginPage />} />

              <Route element={<RequireAuth />}>
                <Route element={<Layout />}>
                  <Route path="/" element={<CameraListPage />} />
                  <Route path="/cameras/:id/view" element={<CameraViewPage />} />
                  <Route path="/archive" element={<ArchivePage />} />
                  <Route path="/events" element={<EventsPage />} />
                </Route>
              </Route>
            </Routes>
          </AuthProvider>
        </BrowserRouter>
      </ToastProvider>
    </QueryClientProvider>
  );
}

export default App;