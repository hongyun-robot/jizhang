import { Form, Segmented } from 'antd';

export default function PrincipalRemainderField() {
  return (
    <Form.Item
      name="principal_remainder"
      label="分期本金尾差"
      rules={[{ required: true }]}
      extra="按分平均分摊，多出的尾差集中到所选期次。仅影响新建分期。"
    >
      <Segmented
        block
        options={[
          { value: 'first', label: '放在首期' },
          { value: 'last', label: '放在最后一期' },
        ]}
      />
    </Form.Item>
  );
}
