export type UserRole = 'admin' | 'operator' | 'viewer' | 'auditor';

export interface AuthUser {
  id: string;
  username: string;
  role: UserRole;
}

export interface AuthTokens {
  access_token: string;
  refresh_token: string;
  user: AuthUser;
}

export interface LoginPayload {
  username: string;
  password: string;
}