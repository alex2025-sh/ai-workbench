// 开发环境的身份切换：从 devgw 的 /dev/users 读取 mock 人员，选中的身份写入 X-Dev-User。
// 接入公司 gateway 后身份由 gateway 认证注入，这里改为读取登录信息即可。
import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { listDevUsers, setUser, type DevUser } from './api/client';

const STORAGE_KEY = 'wb.devUser';

interface Identity {
  user: string;
  users: DevUser[];
  setUser: (id: string) => void;
  error?: string;
}

const Ctx = createContext<Identity>({ user: '', users: [], setUser: () => {} });

function stored(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) ?? '';
  } catch {
    return '';
  }
}

export function IdentityProvider({ children }: { children: ReactNode }) {
  const [user, setCurrent] = useState(() => {
    const u = stored();
    setUser(u);
    return u;
  });
  const [users, setUsers] = useState<DevUser[]>([]);
  const [error, setError] = useState<string>();

  useEffect(() => {
    listDevUsers()
      .then((list) => {
        setUsers(list);
        if (!stored() && list.length > 0) change(list[0].id);
      })
      .catch((e: Error) => setError(e.message));
  }, []); // 只在启动时读取一次身份列表

  const change = (id: string) => {
    setUser(id);
    setCurrent(id);
    try {
      localStorage.setItem(STORAGE_KEY, id);
    } catch {
      // 隐私模式等场景下存储不可用：只在本次会话生效
    }
  };

  return <Ctx.Provider value={{ user, users, setUser: change, error }}>{children}</Ctx.Provider>;
}

export const useIdentity = () => useContext(Ctx);
