import AccessButton from '@/components/common/AccessButton';
import { useRef, useState } from 'react';
import type { ProColumns, ActionType } from '@ant-design/pro-components';
import TableActions from '@/components/common/TableActions';
import ListTable from '@/components/common/ListTable';
import { Alert, Empty, Tag, App } from 'antd';
import { EditOutlined, DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { externalservicev1_DemoContact as DemoContact } from '@/api/generated/admin/service/v1';
import { PaginationQuery } from '@/core';
import { TABLE } from '@/config/constants';
import {
  fetchListExternalDemoContacts,
  useDeleteExternalDemoContact,
} from '@/api/hooks/external-demo-contact';
import ContentContainer from '@/layouts/components/PageContainer/ContentContainer';
import { EXTERNAL_SOURCE_REASON, getErrorReason } from '@/utils/external-source';
import { getActiveMap, getActiveOptions } from './constants';
import ExternalDemoContactDrawer from './components/ExternalDemoContactDrawer';
import { searchOptionRender } from '@/components/common/searchSubmitter';
import { ColumnSlidersIcon } from '@/components/common/columnSettingIcon';

/**
 * 外部数据示例：联系人（外部数据源 demo）
 * 外部数据页面的范例，与普通 CRUD 页的差异见 docs/crud_module_guide/external.md「前端」。
 */
const ExternalDemoContactManagement = () => {
  const { t } = useTranslation('external-demo-contact');
  const actionRef = useRef<ActionType>(null);
  const queryClient = useQueryClient();
  const { message } = App.useApp();

  // 数据源未配置：列表区域换成配置说明，不当作加载失败
  const [notConfigured, setNotConfigured] = useState(false);

  // Drawer 状态管理
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerMode, setDrawerMode] = useState<'create' | 'edit'>('create');
  const [selectedContact, setSelectedContact] = useState<DemoContact | undefined>();

  const activeMap = getActiveMap(t);

  // 删除操作（数据源只读时 error.message 为「外部数据源为只读，不能修改数据」）
  const deleteMutation = useDeleteExternalDemoContact({
    onSuccess: () => {
      message.success(t('deleteSuccess'));
      actionRef.current?.reload();
      queryClient.invalidateQueries({ queryKey: ['listExternalDemoContacts'] });
    },
    onError: (error: Error) => {
      message.error(error.message || t('deleteFailed'));
    },
  });

  // 列配置
  const columns: ProColumns<DemoContact>[] = [
    {
      title: t('name'),
      dataIndex: 'name',
      width: 160,
    },
    {
      title: t('code'),
      dataIndex: 'code',
      width: 140,
    },
    {
      title: t('isActive'),
      dataIndex: 'isActive',
      width: 95,
      valueType: 'select',
      fieldProps: {
        options: getActiveOptions(t),
      },
      render: (_, record) => {
        const config = activeMap[record.isActive ? 'true' : 'false'];
        return <Tag color={config.color}>{config.text}</Tag>;
      },
    },
    {
      // 金额是 decimal 字符串，原样展示，不经过 Number/toFixed
      title: t('amount'),
      dataIndex: 'amount',
      width: 140,
      align: 'right',
      hideInSearch: true,
    },
    {
      title: t('remark'),
      dataIndex: 'remark',
      hideInSearch: true,
      ellipsis: true,
    },
    {
      title: t('updatedAt'),
      dataIndex: 'updatedAt',
      width: 180,
      valueType: 'dateTime',
      hideInSearch: true,
    },
    {
      title: t('action'),
      valueType: 'option',
      width: 150,
      fixed: 'right',
      render: (_, record) => (
        <TableActions
          record={record}
          actions={[
            {
              key: 'edit',
              code: 'external-demo-contact:edit',
              label: t('common:button.edit'),
              icon: <EditOutlined />,
              onClick: () => {
                setDrawerMode('edit');
                setSelectedContact(record);
                setDrawerOpen(true);
              },
            },
            {
              key: 'delete',
              code: 'external-demo-contact:delete',
              label: t('common:button.delete'),
              icon: <DeleteOutlined />,
              danger: true,
              confirm: {
                title: t('deleteConfirmTitle'),
                description: t('deleteConfirmDesc', { name: record.name || '' }),
              },
              onClick: () => record.id && deleteMutation.mutate({ id: record.id }),
            },
          ]}
        />
      ),
    },
  ];

  return (
    <>
      <ContentContainer heightMode="fixed" padding="16px" bottomMargin={0}>
        <Alert className="mb-3" type="info" showIcon title={t('notice')} />
        <ListTable<DemoContact>
          actionRef={actionRef}
          columns={columns}
          request={async (params) => {
            const query = new PaginationQuery({
              paging: {
                page: params.current || 1,
                pageSize: params.pageSize || TABLE.DEFAULT_PAGE_SIZE,
              },
              formValues: Object.fromEntries(
                Object.entries(params).filter(([key]) => !['current', 'pageSize'].includes(key)),
              ),
            });

            try {
              const response = await fetchListExternalDemoContacts(query);
              setNotConfigured(false);
              return {
                data: response.items || [],
                total: response.total || 0,
                success: true,
              };
            } catch (error) {
              // 只接住「未配置」：它不是故障，而是部署缺配置，空状态里告诉管理员去配。
              // 其余错误（不可用/超时等）照常抛给 ListTable 显示错误态 + 重试。
              if (getErrorReason(error) === EXTERNAL_SOURCE_REASON.NOT_CONFIGURED) {
                console.warn('[ExternalDemoContact] 外部数据源 demo 未配置', error);
                setNotConfigured(true);
                return { data: [], total: 0, success: true };
              }
              throw error;
            }
          }}
          locale={
            notConfigured
              ? {
                  emptyText: (
                    <Empty
                      image={Empty.PRESENTED_IMAGE_SIMPLE}
                      description={
                        <>
                          <div>{t('notConfiguredTitle')}</div>
                          <div>{t('notConfiguredDesc')}</div>
                        </>
                      }
                    />
                  ),
                }
              : undefined
          }
          rowKey="id"
          search={{ optionRender: searchOptionRender, labelWidth: 'auto', defaultCollapsed: false }}
          pagination={{
            defaultPageSize: TABLE.DEFAULT_PAGE_SIZE,
            showSizeChanger: true,
            showQuickJumper: true,
          }}
          toolBarRender={() => [
            <AccessButton
              resource="external-demo-contact"
              action="create"
              key="create"
              color="primary"
              variant="outlined"
              icon={<PlusOutlined />}
              onClick={() => {
                setDrawerMode('create');
                setSelectedContact(undefined);
                setDrawerOpen(true);
              }}
            >
              {t('common:button.add')}
            </AccessButton>,
          ]}
          options={{
            density: true,
            fullScreen: true,
            setting: { settingIcon: <ColumnSlidersIcon /> },
            reload: true,
          }}
          size="middle"
          bordered
          cardBordered={false}
        />
      </ContentContainer>

      <ExternalDemoContactDrawer
        open={drawerOpen}
        mode={drawerMode}
        data={selectedContact}
        onClose={() => {
          setDrawerOpen(false);
          setSelectedContact(undefined);
        }}
        onSuccess={() => {
          actionRef.current?.reload();
        }}
      />
    </>
  );
};

export default ExternalDemoContactManagement;
