export function formatDate(dateString: string | undefined) {
    if (!dateString) return ''
    return new Date(dateString).toLocaleString()
}

// 格式化日期时间
export const formatDateTime = (timestamp?: any) => {
    if (!timestamp) return '-';
    try {
        let date: Date;
        if (timestamp.seconds) {
            date = new Date(timestamp.seconds * 1000);
        } else if (typeof timestamp === 'string') {
            date = new Date(timestamp);
        } else {
            return '-';
        }
        return date.toLocaleString('zh-CN', {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
        });
    } catch {
        // 时间戳字段异常时不把错误抛进列表渲染，单元格显示占位符。这是本地展示兜底。
        return '-';
    }
};

// 日期格式化
export const DATE_FORMAT = 'YYYY-MM-DD';
export const TIME_FORMAT = 'YYYY-MM-DD HH:mm:ss';
export const TIME_PICKER_FORMAT = 'HH:mm:ss';
