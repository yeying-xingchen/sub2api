export default {
  pelicanTests: {
    title: '鹈鹕测智',
    description: '让模型直接画一只骑自行车的鹈鹕（HTML/SVG），用作品完整度直观判断账号当前是否降智。核心版为账号级手动测试，作品保存在历史中。',
    form: {
      title: '发起测试',
      account: '测试账号',
      accountPlaceholder: '选择账号',
      model: '模型',
      modelPlaceholder: '留空使用账号默认测试模型',
      prompt: '题目',
      promptPlaceholder: '描述要生成的画面',
      resetPrompt: '恢复默认题目',
      run: '开始测试',
      running: '生成中…'
    },
    history: {
      title: '测试记录',
      empty: '还没有测试记录。选择账号并开始测试后，作品会出现在这里。',
      id: 'ID',
      account: '账号',
      model: '模型',
      status: '结果',
      latency: '耗时',
      createdAt: '时间',
      actions: '操作',
      view: '查看作品'
    },
    status: {
      success: '成功',
      failed: '失败'
    },
    messages: {
      done: '测试完成，作品已保存',
      failed: '测试失败：{error}',
      loadFailed: '加载测试记录失败：{error}',
      loadAccountsFailed: '加载账号失败：{error}',
      selectAccount: '请先选择测试账号'
    },
    preview: {
      title: '鹈鹕作品',
      loading: '作品加载中…',
      loadFailed: '作品加载失败',
      empty: '这次运行没有生成可展示的作品',
      sandboxNote: '作品在隔离沙箱中运行，不能联网。',
      close: '关闭'
    }
  }
}
