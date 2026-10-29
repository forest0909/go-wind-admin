import { Button, type ButtonProps } from 'antd';
import { useAccess } from '@/core/access';

interface AccessButtonProps extends ButtonProps {
  resource: string;
  action: 'create' | 'edit' | 'delete';
}

/** 使用与接口同步一致的资源名控制 CRUD 按钮。 */
export default function AccessButton({ resource, action, ...props }: AccessButtonProps) {
  const { hasAccessByCodes } = useAccess();
  if (!hasAccessByCodes([`${resource}:${action}`])) return null;
  return <Button {...props} />;
}
