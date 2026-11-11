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
    plans: {
      title: '定时计划',
      empty: '还没有定时计划。建一个后，账号会按 Cron 自动跑鹈鹕测智。',
      create: '新建计划',
      edit: '编辑',
      delete: '删除',
      runNow: '立即运行',
      running: '运行中…',
      enabled: '启用',
      paused: '已暂停',
      account: '账号',
      model: '模型',
      cron: 'Cron',
      nextRun: '下次运行',
      lastRun: '最近运行',
      lastStatus: '最近结果',
      never: '尚未运行',
      actions: '操作',
      deleteTitle: '删除定时计划',
      deleteConfirm: '删除后该计划不再自动运行，已保存的作品与历史不受影响。确定删除吗？',
      messages: {
        created: '定时计划已创建',
        updated: '定时计划已保存',
        deleted: '定时计划已删除',
        saveFailed: '保存计划失败：{error}',
        deleteFailed: '删除计划失败：{error}',
        runFailed: '立即运行失败：{error}',
        runDone: '计划已执行，作品已保存'
      },
      editor: {
        createTitle: '新建定时计划',
        editTitle: '编辑定时计划',
        account: '账号',
        model: '模型',
        prompt: '题目',
        cron: 'Cron 表达式',
        cronHint: '标准 5 字段 Cron，按服务器时区执行。',
        enabled: '启用',
        save: '保存',
        cancel: '取消',
        required: '请选择账号并填写 Cron 表达式'
      }
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
