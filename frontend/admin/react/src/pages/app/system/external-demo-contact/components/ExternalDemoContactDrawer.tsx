import { useRef, useState } from 'react';
import type { ProFormInstance } from '@ant-design/pro-components';
import {
  DrawerForm,
  ProFormText,
  ProFormSwitch,
  ProFormTextArea,
} from '@ant-design/pro-components';
import { App } from 'antd';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { externalservicev1_DemoContact as DemoContact } from '@/api/generated/admin/service/v1';
import { useDrawerCloseGuard } from '@/components/common/use-drawer-close-guard';
import {
  useCreateExternalDemoContact,
  useUpdateExternalDemoContact,
} from '@/api/hooks/external-demo-contact';
import { AMOUNT_PATTERN } from '../constants';

interface ExternalDemoContactDrawerProps {
  open: boolean;
  mode: 'create' | 'edit';
  data?: DemoContact;
  onClose: () => void;
  onSuccess: () => void;
}

/**
 * 外部数据示例（联系人）编辑/创建抽屉
 */
const ExternalDemoContactDrawer: React.FC<ExternalDemoContactDrawerProps> = ({
  open,
  mode,
  data,
  onClose,
  onSuccess,
}) => {
  const { t } = useTranslation('external-demo-contact');
  const formRef = useRef<ProFormInstance>(null);

  // 关闭守卫：遮罩/ESC/X/取消统一，脏数据先确认（docs/form_drawer_close_guard.md）
  const { guardClose, handleValuesChange } = useDrawerCloseGuard(open, onClose);
  const queryClient = useQueryClient();
  const { message } = App.useApp();

  const [confirmLoading, setConfirmLoading] = useState(false);

  // 写操作失败（含数据源只读）时 error.message 已是 reason 的译文，直接提示
  const createMutation = useCreateExternalDemoContact({
    onSuccess: () => {
      message.success(t('createSuccess'));
      onSuccess();
      onClose();
      queryClient.invalidateQueries({ queryKey: ['listExternalDemoContacts'] });
    },
    onError: (error: Error) => {
      message.error(error.message || t('createFailed'));
    },
  });

  const updateMutation = useUpdateExternalDemoContact({
    onSuccess: () => {
      message.success(t('updateSuccess'));
      onSuccess();
      onClose();
      queryClient.invalidateQueries({ queryKey: ['listExternalDemoContacts'] });
    },
    onError: (error: Error) => {
      message.error(error.message || t('updateFailed'));
    },
  });

  const handleSubmit = async (values: DemoContact) => {
    setConfirmLoading(true);

    try {
      // 金额保持字符串；清空时不传值（更新时随 updateMask 置空）
      const amount = values.amount?.trim();
      const payload = {
        ...values,
        isActive: values.isActive ?? true,
        amount: amount || undefined,
      };

      if (mode === 'create') {
        await createMutation.mutateAsync({ data: payload });
      } else if (data?.id) {
        await updateMutation.mutateAsync({ id: data.id, values: payload });
      }
    } finally {
      setConfirmLoading(false);
    }
  };

  return (
    <DrawerForm
      formRef={formRef}
      title={mode === 'create' ? t('create') : t('edit')}
      open={open}
      onValuesChange={handleValuesChange}
      initialValues={
        mode === 'edit'
          ? {
              name: data?.name,
              code: data?.code,
              isActive: data?.isActive ?? false,
              amount: data?.amount,
              remark: data?.remark,
            }
          : { isActive: true }
      }
      onFinish={handleSubmit}
      submitter={{
        searchConfig: {
          submitText: t('common:button.submit'),
          resetText: t('common:button.cancel'),
        },
        submitButtonProps: {
          loading: confirmLoading || createMutation.isPending || updateMutation.isPending,
        },
        resetButtonProps: {
          onClick: guardClose,
        },
      }}
      drawerProps={{
        destroyOnHidden: true,
        onClose: guardClose,
        size: 480,
      }}
      layout="horizontal"
      labelCol={{ flex: '0 0 100px' }}
      wrapperCol={{ flex: 'auto' }}
    >
      <ProFormText
        name="name"
        label={t('name')}
        placeholder={t('namePlaceholder')}
        rules={[{ required: true, message: t('requiredName') }]}
        fieldProps={{ allowClear: true }}
      />

      <ProFormText
        name="code"
        label={t('code')}
        placeholder={t('codePlaceholder')}
        rules={[{ required: true, message: t('requiredCode') }]}
        fieldProps={{ allowClear: true }}
      />

      {/* 金额用文本框 + 十进制校验，不用数字框：数字框会把值变成 number，大金额丢精度 */}
      <ProFormText
        name="amount"
        label={t('amount')}
        placeholder={t('amountPlaceholder')}
        rules={[{ pattern: AMOUNT_PATTERN, message: t('amountInvalid') }]}
        fieldProps={{ allowClear: true, inputMode: 'decimal' }}
      />

      <ProFormSwitch name="isActive" label={t('isActive')} />

      <ProFormTextArea
        name="remark"
        label={t('remark')}
        placeholder={t('remarkPlaceholder')}
        fieldProps={{ rows: 3 }}
      />
    </DrawerForm>
  );
};

export default ExternalDemoContactDrawer;
