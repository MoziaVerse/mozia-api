/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Checkbox,
  Form,
  Modal,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { Plus, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';

const { Text } = Typography;

const sourceOptions = [
  { label: '赠送额度', value: 'gift' },
  { label: '充值额度', value: 'paid' },
  { label: '历史额度', value: 'legacy' },
];

const matchTypeOptions = [
  { label: '精确匹配', value: 'exact' },
  { label: '前缀匹配', value: 'prefix' },
  { label: '通配符匹配', value: 'wildcard' },
];

const consumeOrderOptions = [
  { label: '优先赠送额度', value: 'gift_first' },
  { label: '优先充值额度', value: 'paid_first' },
];

const emptyForm = {
  model_pattern: '',
  match_type: 'exact',
  allowed_sources: ['gift', 'paid', 'legacy'],
  consume_order: 'gift_first',
  enabled: true,
  priority: 0,
};

function parseSources(value) {
  const sources = String(value || '')
    .split(',')
    .map((item) => item.trim())
    .filter((item) => ['gift', 'paid', 'legacy'].includes(item));
  return sources.length > 0 ? sources : ['gift', 'paid', 'legacy'];
}

function sourceLabel(source) {
  return sourceOptions.find((item) => item.value === source)?.label || source;
}

function optionLabel(options, value) {
  return options.find((item) => item.value === value)?.label || value;
}

function policyToForm(policy) {
  return {
    id: policy.id,
    model_pattern: policy.model_pattern,
    match_type: policy.match_type,
    allowed_sources: parseSources(policy.allowed_sources),
    consume_order: policy.consume_order,
    enabled: Boolean(policy.enabled),
    priority: policy.priority || 0,
  };
}

function formToPayload(form) {
  return {
    model_pattern: String(form.model_pattern || '').trim(),
    match_type: form.match_type,
    allowed_sources: form.allowed_sources.join(','),
    consume_order: form.consume_order,
    enabled: Boolean(form.enabled),
    priority: Number(form.priority) || 0,
  };
}

const MoziaQuotaPolicySetting = () => {
  const { t } = useTranslation();
  const [policies, setPolicies] = useState([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);
  const [form, setForm] = useState(emptyForm);

  const sortedPolicies = useMemo(
    () =>
      [...policies].sort((a, b) => {
        if ((a.priority || 0) !== (b.priority || 0)) {
          return (b.priority || 0) - (a.priority || 0);
        }
        return (b.id || 0) - (a.id || 0);
      }),
    [policies],
  );

  const loadPolicies = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/mozia/quota-policy/');
      const { success, data, message } = res.data;
      if (success) {
        setPolicies(data || []);
      } else {
        showError(message || t('加载失败'));
      }
    } catch (error) {
      showError(t('加载失败'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadPolicies();
  }, []);

  const openCreateModal = () => {
    setForm({ ...emptyForm });
    setModalVisible(true);
  };

  const openEditModal = (policy) => {
    setForm(policyToForm(policy));
    setModalVisible(true);
  };

  const submitPolicy = async () => {
    if (!form.model_pattern.trim()) {
      showError(t('请填写模型匹配规则'));
      return;
    }
    if (!form.allowed_sources.length) {
      showError(t('请至少选择一种额度来源'));
      return;
    }
    setSaving(true);
    try {
      const payload = formToPayload(form);
      const res = form.id
        ? await API.put(`/api/mozia/quota-policy/${form.id}`, payload)
        : await API.post('/api/mozia/quota-policy/', payload);
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('保存成功'));
        setModalVisible(false);
        await loadPolicies();
      } else {
        showError(message || t('保存失败'));
      }
    } catch (error) {
      showError(t('保存失败'));
    } finally {
      setSaving(false);
    }
  };

  const deletePolicy = (policy) => {
    Modal.confirm({
      title: t('删除额度策略？'),
      content: t('删除后该策略将不再参与模型额度来源判断。'),
      okType: 'danger',
      onOk: async () => {
        try {
          const res = await API.delete(`/api/mozia/quota-policy/${policy.id}`);
          const { success, message } = res.data;
          if (success) {
            showSuccess(t('删除成功'));
            await loadPolicies();
          } else {
            showError(message || t('删除失败'));
          }
        } catch (error) {
          showError(t('删除失败'));
        }
      },
    });
  };

  const columns = [
    {
      title: t('模型匹配规则'),
      dataIndex: 'model_pattern',
      render: (text) => <Text code>{text}</Text>,
    },
    {
      title: t('匹配方式'),
      dataIndex: 'match_type',
      render: (text) => t(optionLabel(matchTypeOptions, text)),
    },
    {
      title: t('可用额度来源'),
      dataIndex: 'allowed_sources',
      render: (text) => (
        <Space wrap>
          {parseSources(text).map((source) => (
            <Tag key={source} color={source === 'paid' ? 'blue' : 'grey'}>
              {t(sourceLabel(source))}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: t('扣减顺序'),
      dataIndex: 'consume_order',
      render: (text) => t(optionLabel(consumeOrderOptions, text)),
    },
    {
      title: t('优先级'),
      dataIndex: 'priority',
      width: 90,
    },
    {
      title: t('状态'),
      dataIndex: 'enabled',
      width: 90,
      render: (enabled) => (
        <Tag color={enabled ? 'green' : 'grey'}>
          {enabled ? t('启用') : t('禁用')}
        </Tag>
      ),
    },
    {
      title: t('操作'),
      width: 160,
      render: (_, record) => (
        <Space>
          <Button size='small' onClick={() => openEditModal(record)}>
            {t('编辑')}
          </Button>
          <Button
            size='small'
            type='danger'
            onClick={() => deletePolicy(record)}
          >
            {t('删除')}
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <Spin spinning={loading}>
      <Card style={{ marginTop: 10 }}>
        <Banner
          type='info'
          fullMode={false}
          title={t('模型额度策略说明')}
          description={
            <div>
              <div>
                {t(
                  '内置余额仍显示 users.quota 总余额；模型调用时会按策略限制 gift、paid、legacy 的可用性。',
                )}
              </div>
              <div>
                {t(
                  '没有命中策略的模型默认允许所有额度来源。高成本模型建议配置为 paid-only。',
                )}
              </div>
              <pre className='mt-2 whitespace-pre-wrap rounded bg-gray-50 p-2 text-xs'>
                {`高成本示例:
model_pattern: claude-*
match_type: wildcard
allowed_sources: paid
consume_order: paid_first
priority: 100

低成本示例:
model_pattern: gpt-4o-mini
match_type: exact
allowed_sources: gift,paid,legacy
consume_order: gift_first
priority: 10`}
              </pre>
            </div>
          }
        />

        <div className='my-4 flex flex-wrap justify-between gap-2'>
          <Button icon={<RefreshCw size={14} />} onClick={loadPolicies}>
            {t('刷新')}
          </Button>
          <Button theme='solid' icon={<Plus size={14} />} onClick={openCreateModal}>
            {t('新增策略')}
          </Button>
        </div>

        <Table
          rowKey='id'
          columns={columns}
          dataSource={sortedPolicies}
          pagination={false}
        />
      </Card>

      <Modal
        title={form.id ? t('编辑额度策略') : t('新增额度策略')}
        visible={modalVisible}
        onCancel={() => setModalVisible(false)}
        onOk={submitPolicy}
        confirmLoading={saving}
        style={{ maxWidth: 720 }}
      >
        <Form labelPosition='left' labelWidth={130}>
          <Form.Input
            label={t('模型匹配规则')}
            field='model_pattern'
            value={form.model_pattern}
            placeholder='gpt-4o-mini'
            onChange={(value) => setForm({ ...form, model_pattern: value })}
          />
          <Form.Select
            label={t('匹配方式')}
            field='match_type'
            value={form.match_type}
            optionList={matchTypeOptions.map((item) => ({
              label: t(item.label),
              value: item.value,
            }))}
            onChange={(value) => setForm({ ...form, match_type: value })}
          />
          <Form.Select
            label={t('扣减顺序')}
            field='consume_order'
            value={form.consume_order}
            optionList={consumeOrderOptions.map((item) => ({
              label: t(item.label),
              value: item.value,
            }))}
            onChange={(value) => setForm({ ...form, consume_order: value })}
          />
          <Form.InputNumber
            label={t('优先级')}
            field='priority'
            value={form.priority}
            onChange={(value) => setForm({ ...form, priority: value || 0 })}
          />
          <Form.Switch
            label={t('启用')}
            field='enabled'
            checked={form.enabled}
            onChange={(checked) => setForm({ ...form, enabled: checked })}
          />
          <Form.Slot label={t('可用额度来源')}>
            <Checkbox.Group
              value={form.allowed_sources}
              onChange={(value) => setForm({ ...form, allowed_sources: value })}
            >
              {sourceOptions.map((source) => (
                <Checkbox key={source.value} value={source.value}>
                  {t(source.label)}
                </Checkbox>
              ))}
            </Checkbox.Group>
          </Form.Slot>
        </Form>
      </Modal>
    </Spin>
  );
};

export default MoziaQuotaPolicySetting;
