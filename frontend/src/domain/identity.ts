export type Role = 'admin' | 'recorder' | 'member' | 'viewer';
export type UserStatus = 'active' | 'disabled';
export interface SessionUser {
  id: number;
  username: string;
  role: Role;
  status: UserStatus;
}
