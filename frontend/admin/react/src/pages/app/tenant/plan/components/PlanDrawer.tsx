import { useRef, useState, useEffect, useMemo } from 'react';
import type { ProFormInstance } from '@ant-design/pro-components';
import {
  DrawerForm,
  ProFormText,
  ProFormSelect,
  ProFormDigit,
  ProFormTextArea,
} from '@ant-design/pro-components';
import { App } from 'antd';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import {
  useCreatePlan,
  useUpdatePlan,
  fetchListPlans,
  useListPlanModules,
  useCreatePlanModule,
  useDeletePlanModule,
} from '@/api/hooks/plan';
import { PaginationQuery } from '@/core/transport/rest';
import { useDrawerCloseGuard } from '@/components/common/use-drawer-close-guard';
import { getPlanVersionOptions, getExpiryPolicyOptions, getModuleOptions } from '../constants';
import { SELECT_FILTER_PROPS } from '../constants';

// 将后端 PlanModule 列表映射为前端多选值（字符串模块名）。
// 后端 entity 的 module 字段为字符串枚举值，与 MODULE_LABEL_KEYS 中的字符串一致。
function extractModuleNames(items: any[] | undefined): string[] {
  const names: string[] = [];
  if (items) {
    for (const item of items) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const mod = (item as any)?.module;
      if (typeof mod === 'string' && mod.length > 0) {
        names.push(mod);
      }
    }
  }
  return names;
}

interface PlanDrawerProps {
  open: boolean;
  mode: 'create' | 'edit';
  data?: any;
  onClose: () => void;
  onSuccess: () => void;
}

/**
 * 套餐目录编辑/创建抽屉组件
 */
const PlanDrawer: React.FC<PlanDrawerProps> = ({
  open,
  mode,
  data,
  onClose,
  onSuccess,
}) => {
  const { t } = useTranslation('plan');
  const formRef = useRef<ProFormInstance>(null);

  // 关闭守卫：遮罩/ESC/X/取消统一，脏数据先确认（docs/form_drawer_close_guard.md）
  const { guardClose, handleValuesChange } = useDrawerCloseGuard(open, onClose);
  const queryClient = useQueryClient();
  const { message } = App.useApp();

  const [confirmLoading, setConfirmLoading] = useState(false);

  // 编辑模式下加载套餐的模块白名单。
  // 通过 plan_id 过滤查询 sys_plan_modules，提取 module 字段填充多选框。
  const moduleIdQuery = new PaginationQuery({
    paging: { page: 1, pageSize: 999 },
    formValues: mode === 'edit' && data?.id ? { plan_id: data.id } : {},
  });
  const { data: planModuleData } = useListPlanModules(moduleIdQuery, {
    enabled: open && mode === 'edit' && !!data?.id,
  });
  // 必须 memo：每次渲染都新建数组会让下面的回填 effect 每次渲染都重跑，冲掉用户正在改的值
  const existingModules = useMemo(
    () => extractModuleNames(planModuleData?.items),
    [planModuleData],
  );

  // 编辑模式下设置表单值（destroyOnHidden 时需延迟赋值）
  useEffect(() => {
    if (open && mode === 'edit' && data) {
      setTimeout(() => {
        formRef.current?.setFieldsValue({
          name: data.name || '',
          version: data.version,
          expiryPolicy: data.expiryPolicy,
          dataRetentionDays: data.dataRetentionDays,
          description: data.description || '',
          remark: data.remark || '',
          moduleWhitelist: existingModules,
        });
      }, 0);
    }
  }, [open, mode, data, existingModules]);

  // 创建/更新成功后的提示与关闭放在 handleSubmit 里，等模块白名单同步完再做，
  // 否则抽屉先关掉卸载，白名单请求还没发出去。
  const createMutation = useCreatePlan({
    onError: (error: Error) => {
      message.error(error.message || t('createFailed'));
    },
  });
  const updateMutation = useUpdatePlan({
    onError: (error: Error) => {
      message.error(error.message || t('updateFailed'));
    },
  });

  // 模块白名单 CRUD mutations
  const createModuleMutation = useCreatePlanModule();
  const deleteModuleMutation = useDeletePlanModule();

  // Create 接口不回传 id。套餐名有唯一索引（uix_sys_plans_name），按名称查回新建的那一条。
  const findPlanIdByName = async (name: string): Promise<number | undefined> => {
    const resp = await fetchListPlans(
      new PaginationQuery({ paging: { page: 1, pageSize: 100 }, formValues: { name } }),
    );
    // name 查询走 contains，可能带出同前缀的其他套餐，这里再精确比对
    return resp?.items?.find((it) => it?.name === name)?.id;
  };

  // 同步模块白名单：计算 diff（新增、删除），逐一调用 create/delete。
  const syncModules = async (planId: number, selected: string[], existing: string[]) => {
    const toAdd = selected.filter((m) => !existing.includes(m));
    const toRemove = existing.filter((m) => !selected.includes(m));

    for (const mod of toAdd) {
      await createModuleMutation.mutateAsync({
        data: { planId, module: mod as any },
      } as any);
    }
    for (const mod of toRemove) {
      // 查找该模块对应的 plan_module 记录 ID 以执行删除。
      const item = planModuleData?.items?.find(
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (it: any) => it?.module === mod,
      );
      if (item?.id) {
        await deleteModuleMutation.mutateAsync({ id: item.id } as any);
      }
    }
  };

  // 提交表单
  const handleSubmit = async (values: Record<string, any>) => {
    setConfirmLoading(true);

    // 剥离模块白名单字段，不传入套餐主体 CRUD。
    const { moduleWhitelist, ...planValues } = values;
    const selectedModules: string[] = Array.isArray(moduleWhitelist) ? moduleWhitelist : [];
    const isEdit = mode === 'edit' && !!data?.id;

    try {
      if (isEdit) {
        await updateMutation.mutateAsync({ id: data.id, values: planValues });
      } else {
        await createMutation.mutateAsync({ data: planValues });
      }
    } catch (error) {
      // 失败提示已由 mutation 的 onError 弹出，这里只记原始错误
      console.error('save plan failed', error);
      setConfirmLoading(false);
      return false;
    }

    // 套餐主体已经保存。白名单失败时也要刷新列表并关闭抽屉，提示用户去编辑页补救，
    // 否则再点一次提交会因为重名而创建失败。
    try {
      const planId = isEdit ? data.id : await findPlanIdByName(planValues.name);
      if (!planId) {
        throw new Error(`created plan not found by name: ${planValues.name}`);
      }
      await syncModules(planId, selectedModules, isEdit ? existingModules : []);
      message.success(t(isEdit ? 'updateSuccess' : 'createSuccess'));
    } catch (error) {
      console.error('sync plan modules failed', error);
      message.error(t('moduleSyncFailed'));
    } finally {
      queryClient.invalidateQueries({ queryKey: ['listPlans'] });
      queryClient.invalidateQueries({ queryKey: ['listPlanModules'] });
      setConfirmLoading(false);
    }

    onSuccess();
    onClose();
    return true;
  };

  return (
    <DrawerForm
      formRef={formRef}
      title={mode === 'create' ? t('create') : t('edit')}
      open={open}
      onValuesChange={handleValuesChange}
      onFinish={handleSubmit}
      submitter={{
        searchConfig: {
          submitText: t('common:button.submit'),
          resetText: t('common:button.cancel'),
        },
        submitButtonProps: {
          loading: confirmLoading || createMutation.isPending || updateMutation.isPending,
        },
        resetButtonProps: { onClick: guardClose },
      }}
      drawerProps={{ destroyOnHidden: true, onClose: guardClose, size: 600 }}
      layout="horizontal"
      labelCol={{ flex: '0 0 120px' }}
      wrapperCol={{ flex: 'auto' }}
    >
      <ProFormText
        name="name"
        label={t('name')}
        placeholder={t('namePlaceholder')}
        rules={[{ required: true, message: t('requiredName') }]}
        fieldProps={{ allowClear: true }}
      />

      <ProFormSelect
        name="version"
        label={t('versionLabel')}
        placeholder={t('versionPlaceholder')}
        options={getPlanVersionOptions(t)}
        rules={[{ required: true, message: t('requiredVersion') }]}
        fieldProps={SELECT_FILTER_PROPS}
      />

      <ProFormSelect
        name="expiryPolicy"
        label={t('expiryPolicyLabel')}
        placeholder={t('expiryPolicyPlaceholder')}
        options={getExpiryPolicyOptions(t)}
        rules={[{ required: true, message: t('requiredExpiryPolicy') }]}
        fieldProps={SELECT_FILTER_PROPS}
      />

      <ProFormSelect
        name="moduleWhitelist"
        label={t('moduleWhitelist')}
        placeholder={t('moduleWhitelistPlaceholder')}
        options={getModuleOptions(t)}
        fieldProps={{ ...SELECT_FILTER_PROPS, mode: 'multiple' }}
      />

      <ProFormDigit
        name="dataRetentionDays"
        label={t('dataRetentionDays')}
        placeholder={t('dataRetentionDaysPlaceholder')}
        fieldProps={{ precision: 0, min: 0 }}
      />

      <ProFormTextArea
        name="description"
        label={t('description')}
        placeholder={t('descriptionPlaceholder')}
        fieldProps={{ allowClear: true, autoSize: { minRows: 2, maxRows: 4 } }}
      />

      <ProFormTextArea
        name="remark"
        label={t('remark')}
        placeholder={t('remarkPlaceholder')}
        fieldProps={{ allowClear: true, autoSize: { minRows: 2, maxRows: 4 } }}
      />
    </DrawerForm>
  );
};

export default PlanDrawer;
