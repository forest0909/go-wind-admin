<script lang="ts" setup>
import { computed, h, ref } from 'vue';

import { useVbenDrawer } from '@vben/common-ui';
import { useFormCloseGuard } from '#/composables/use-form-close-guard';
import { $t } from '@vben/locales';

import { notification } from 'ant-design-vue';

import { useVbenForm } from '#/adapter/form';
import AiGenerateButton from '#/components/AiGenerateButton.vue';
import { type permissionservicev1_PermissionGroup as PermissionGroup } from '#/api';
import {
  buildPermissionTree,
  fetchListOrgUnits,
  fetchListPermissionGroups,
  fetchListPermissions,
  PaginationQuery,
  roleDataScopeList,
  statusList,
  useCreateRole,
  useUpdateRole,
} from '#/api';
import { deepClone, filterNumbers } from '#/utils';
import PermissionCheckTree from '../permission/permission-check-tree.vue';
import RoleFieldPermCard from './role-field-perm-card.vue';

const { mutateAsync: createRole } = useCreateRole();
const { mutateAsync: updateRole } = useUpdateRole();

const data = ref();
const groups = ref<PermissionGroup[]>([]);

// 分配权限勾选树：数据与装载态由抽屉 onOpenChange 拉取（对齐权限点抽屉的
// PermissionCheckTree 用法），提交时勾选 key 数组即权限 ID。
const permTreeData = ref<any[]>([]);
const permTreeLoading = ref(false);

async function loadPermTree() {
  permTreeLoading.value = true;
  try {
    const groupData = await fetchListPermissionGroups(
      new PaginationQuery({
        formValues: { status: 'ON' },
      }),
    );
    groups.value = groupData.items ?? [];
    const permData = await fetchListPermissions(
      new PaginationQuery({
        formValues: { status: 'ON' },
      }),
    );
    permTreeData.value = buildPermissionTree(groups.value, permData.items ?? []);
  } catch (error) {
    console.error('load permission tree failed', error);
    permTreeData.value = [];
  } finally {
    permTreeLoading.value = false;
  }
}

const getTitle = computed(() =>
  data.value?.create
    ? $t('ui.modal.create', { moduleName: $t('page.role.moduleName') })
    : $t('ui.modal.update', { moduleName: $t('page.role.moduleName') }),
);
// const isCreate = computed(() => data.value?.create);

const [BaseForm, baseFormApi] = useVbenForm({
  showDefaultActions: false,
  // 所有表单项共用，可单独在表单内覆盖
  commonConfig: {
    // react 基准抽屉标签列为 120px，这里放宽到 130 容纳「字段权限（User）」整行不折行
    labelWidth: 130,
    // 标签列内左对齐（font 尺寸三端本就同为 14px，字重交由 FormLabel 默认）
    labelClass: 'justify-start',
    // 所有表单项
    componentProps: {
      class: 'w-full',
    },
  },
  schema: [
    {
      component: 'Input',
      fieldName: 'name',
      label: $t('page.role.name'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
      rules: 'required',
    },
    {
      component: 'Input',
      fieldName: 'code',
      label: $t('page.role.code'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
      rules: 'required',
    },
    {
      component: 'InputNumber',
      fieldName: 'sortOrder',
      defaultValue: 1,
      label: $t('ui.table.sortOrder'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
        min: 0,
        precision: 0,
      },
      rules: 'required',
    },
    {
      component: 'RadioGroup',
      fieldName: 'status',
      label: $t('ui.table.status'),
      defaultValue: 'ON',
      rules: 'selectRequired',
      componentProps: {
        optionType: 'button',
        buttonStyle: 'solid',
        class: 'flex flex-wrap', // 如果选项过多，可以添加class来自动折叠
        options: statusList,
      },
    },
    {
      component: 'Select',
      fieldName: 'dataScope',
      label: $t('page.role.dataScope'),
      defaultValue: 'ALL',
      rules: 'selectRequired',
      componentProps: {
        options: roleDataScopeList,
        placeholder: $t('ui.placeholder.select'),
      },
    },
    {
      component: 'ApiTree',
      fieldName: 'orgUnits',
      dependencies: {
        // 仅 SELECTED_UNITS 档展示自定义授权单元集配置
        show: (values) => values.dataScope === 'SELECTED_UNITS',
        triggerFields: ['dataScope'],
      },
      componentProps: {
        title: $t('page.role.orgUnits'),
        treeDefaultExpandAll: true,
        childrenField: 'children',
        labelField: 'name',
        valueField: 'id',
        api: async () => {
          const result = await fetchListOrgUnits(
            new PaginationQuery({
              formValues: { status: 'ON' },
            }),
          );
          return result.items;
        },
      },
    },
    {
      component: 'Textarea',
      fieldName: 'description',
      label: $t('ui.table.description'),
      // 与 react/ele 对齐：AI 生成钮进 label、textarea 占满整行（放 suffix 会把
      // 输入框压掉约 90px 宽），3 行起步比默认的两行高（ele rows:3 同款）
      labelRender: () =>
        h('span', { class: 'inline-flex items-center' }, [
          $t('ui.table.description'),
          h(AiGenerateButton, {
            scene: 'DESCRIPTION',
            onGenerate: (content: string) =>
              baseFormApi.setFieldValue('description', content),
          }),
        ]),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
        rows: 3,
      },
    },
    {
      // 分配权限：直接复用权限点抽屉的 PermissionCheckTree（对齐 ele 权限配置
      // 段的形态——段头顶格左对齐 + 全宽圆角边框盒内滚动 + 空态虚线占位）。
      // hideLabel 让该行脱离 130px 标签列、通栏铺满，段头由组件 title 承担。
      component: PermissionCheckTree,
      fieldName: 'permissions',
      hideLabel: true,
      componentProps: () => ({
        title: $t('page.role.permissions'),
        treeData: permTreeData.value,
        loading: permTreeLoading.value,
        emptyText: $t('common.noData'),
      }),
    },
    {
      // 字段权限：勾选 = 对该角色用户隐藏（黑名单语义）。对齐 react 基准——
      // 「字段权限」段头顶格 + 一张边框卡内 提示语 + User/Role 两组竖排勾选。
      // value 与后端各资源 proto 字段 json_name 逐字一致（当前 User/Role 两资源）。
      component: RoleFieldPermCard,
      fieldName: 'fieldPerm',
      hideLabel: true,
      componentProps: () => ({
        title: $t('page.role.fieldPerm.title'),
        hint: $t('page.role.fieldPerm.hint'),
        userOptions: [
          { label: $t('page.role.fieldPerm.field.email'), value: 'email' },
          { label: $t('page.role.fieldPerm.field.mobile'), value: 'mobile' },
          { label: $t('page.role.fieldPerm.field.telephone'), value: 'telephone' },
          { label: $t('page.role.fieldPerm.field.address'), value: 'address' },
          { label: $t('page.role.fieldPerm.field.region'), value: 'region' },
          { label: $t('page.role.fieldPerm.field.lastLoginAt'), value: 'lastLoginAt' },
          { label: $t('page.role.fieldPerm.field.lastLoginIp'), value: 'lastLoginIp' },
        ],
        roleOptions: [
          {
            label: $t('page.role.fieldPerm.field.permissions'),
            value: 'permissions',
          },
        ],
      }),
    },
  ],
});

// 关闭守卫：遮罩/ESC/X/取消统一，脏数据先确认（docs/form_drawer_close_guard.md）
const closeGuard = useFormCloseGuard(baseFormApi);

const [Drawer, drawerApi] = useVbenDrawer({
  onBeforeClose: () => closeGuard.onBeforeClose(),
  onCancel() {
    drawerApi.close();
  },

  async onConfirm() {

    // 校验输入的数据
    const validate = await baseFormApi.validate();
    if (!validate.valid) {
      return;
    }

    setLoading(true);

    // 获取表单数据
    const values = await baseFormApi.getValues();
    // @ts-ignore JSON.stringify
    const finalValues = deepClone(values);

    if (
      finalValues.permissions !== null &&
      Array.isArray(finalValues.permissions) &&
      finalValues.permissions.length > 0
    ) {
      finalValues.permissions = filterNumbers(values.permissions);
    }

    // 仅 SELECTED_UNITS 档提交授权单元集（含清空场景）；
    // 其余档位不携带该字段，后端维持既有集不替换。
    if (finalValues.dataScope === 'SELECTED_UNITS') {
      finalValues.orgUnits = Array.isArray(values.orgUnits)
        ? filterNumbers(values.orgUnits)
        : [];
    } else {
      delete finalValues.orgUnits;
    }

    // 字段权限始终随表单提交（含清空场景）：抽屉所见即保存后的最终态。
    // fieldPerm 是表单专用中间字段（{ user, role } 两组勾选），不是 Role proto
    // 字段，提交前必须折算成 fieldPermissions 多资源数组并移除。
    finalValues.fieldPermissions = (
      [
        ['User', finalValues.fieldPerm?.user],
        ['Role', finalValues.fieldPerm?.role],
      ] as const
    )
      .filter(([, fields]) => Array.isArray(fields) && fields.length > 0)
      .map(([resource, fields]) => ({
        resource,
        hiddenFields: fields.filter((v: unknown) => typeof v === 'string'),
      }));
    delete finalValues.fieldPerm;


    try {
      await (data.value?.create
        ? createRole(finalValues)
        : updateRole({ id: data.value.row.id, values: finalValues }));

      notification.success({
        message: data.value?.create
          ? $t('ui.notification.create_success')
          : $t('ui.notification.update_success'),
      });
      closeGuard.markClean();
    } catch {
      notification.error({
        message: data.value?.create
          ? $t('ui.notification.create_failed')
          : $t('ui.notification.update_failed'),
      });
    } finally {
      drawerApi.close();
      setLoading(false);
    }
  },

  async onOpenChange(isOpen) {
    if (isOpen) {
      // 获取传入的数据
      data.value = drawerApi.getData<Record<string, any>>();

      // 为表单赋值。注意：setValues 必须只调一次——连调两次时后一次会清掉
      // 前一次刚设置的值（vee-validate setValues 非合并语义），因此把字段权限
      // 的中间态 fieldPerm（User/Role 两组）合并在同一次调用里。
      const row = data.value?.row;
      const findHiddenFields = (resource: string) => {
        const entry = Array.isArray(row?.fieldPermissions)
          ? row.fieldPermissions.find((e: any) => e?.resource === resource)
          : undefined;
        return Array.isArray(entry?.hiddenFields)
          ? entry.hiddenFields.filter((v: any) => typeof v === 'string')
          : [];
      };
      await baseFormApi.setValues({
        ...(row ?? {}),
        fieldPerm: {
          user: findHiddenFields('User'),
          role: findHiddenFields('Role'),
        },
      });
      await closeGuard.syncBaseline();

      // 分配权限勾选树（打开抽屉时拉取，对齐权限点抽屉）
      loadPermTree();

      // setLoading(true);
      setLoading(false);
    }
  },
});

function setLoading(loading: boolean) {
  drawerApi.setState({ loading });
}
</script>

<template>
  <Drawer :title="getTitle">
    <BaseForm />
  </Drawer>
</template>
