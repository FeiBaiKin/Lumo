import { api, problemMessage } from "@/api/client";
import { queryClient } from "@/api/query-client";
import type { components } from "@/api/schema";
import {
  FileAudio,
  FileText,
  FileVideo,
  Image as ImageIcon,
  type LucideIcon,
} from "lucide-react";
import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";

/**
 * 附件的共用部分：类型元信息、缩略图取法、上传。
 *
 * 抽出来的理由是附件库不再只有一个入口：附件页、编辑器的插图弹窗、
 * 封面图与设置表单的图片字段都要列附件、都要能上传。
 * 同一份「怎么取缩略图」「上传失败怎么报」散成四份的话，
 * 很快会出现「附件页显示 medium 档、选择器显示原图」这种不一致。
 *
 * 查询缓存键统一以 `media` 开头（见 useMediaUpload 的失效逻辑）：
 * 任何一处上传完成，其余正开着的列表都会自己刷新。
 */

export type Media = components["schemas"]["Media"];
export type MediaKind = Media["kind"];

export const MEDIA_KIND_META: Record<
  string,
  { label: string; icon: LucideIcon }
> = {
  image: { label: "图片", icon: ImageIcon },
  video: { label: "视频", icon: FileVideo },
  audio: { label: "音频", icon: FileAudio },
  document: { label: "文档", icon: FileText },
  other: { label: "其他", icon: FileText },
};

const FALLBACK_KIND = { label: "文件", icon: FileText };

export const MEDIA_KIND_OPTIONS = [
  { value: "", label: "全部类型" },
  ...Object.entries(MEDIA_KIND_META).map(([value, meta]) => ({
    value,
    label: meta.label,
  })),
];

/** 服务端未来新增 kind 时退回「文件」而不是渲染空白。 */
export function kindOf(media: Media) {
  return MEDIA_KIND_META[media.kind] ?? FALLBACK_KIND;
}

/** 取缩略图：优先 medium 档，退回原图。 */
export function previewOf(media: Media): string {
  const thumbs = media.thumbnails ?? [];
  const medium = thumbs.find((t) => t.name === "medium");
  return medium?.url || thumbs[0]?.url || media.url;
}

/** 补全成完整地址。复制出去的地址要能直接粘到别处用，相对路径在那里不可用。 */
export function absoluteUrl(url: string): string {
  return new URL(url, window.location.origin).href;
}

/** 复制附件地址到剪贴板，并播报结果。 */
export async function copyMediaUrl(media: Media) {
  try {
    await navigator.clipboard.writeText(absoluteUrl(media.url));
    toast.success("已复制地址");
  } catch {
    // 非 HTTPS 或权限被拒时剪贴板不可用
    toast.error("剪贴板不可用，请在详情里手动选中地址复制");
  }
}

/** 上传一个文件。multipart 接口一次只收一个。 */
export async function uploadMedia(file: File): Promise<Media> {
  const body = new FormData();
  body.append("file", file);
  const { data, error, response } = await api.POST("/api/v1/console/media", {
    /*
     * openapi-fetch 在解析 multipart 请求体时按 schema 生成对象，
     * 但真实上传必须传 FormData 才能带上文件流。
     * `bodySerializer` 直接返回 FormData 即绕开序列化 ——
     * 类型上需要两次断言，因为生成的类型是「字段名 → 值」的对象。
     */
    body: body as unknown as { file: string },
    bodySerializer: () => body,
  });
  if (!response.ok || !data) {
    throw new Error(problemMessage(error));
  }
  return data;
}

/**
 * 批量上传。
 *
 * 逐个传而不是并发：multipart 接口一次只收一个文件，而并发几个大图
 * 会把上行带宽占满，反而都变慢。某一个失败只报它自己，其余继续。
 *
 * 成功后统一失效 `["media"]` 前缀的查询，调用方不必再手动 refetch ——
 * 在选择器里传完的图，背后的附件页再回去看也是新的。
 */
export function useMediaUpload(
  options: { onUploaded?: ((items: Media[]) => void) | undefined } = {},
) {
  const [uploading, setUploading] = useState(false);
  // 回调放 ref：否则每次渲染换一个新函数，uploadFiles 的身份就跟着变
  const uploaded = useRef(options.onUploaded);
  uploaded.current = options.onUploaded;

  const uploadFiles = useCallback(
    async (files: FileList | File[]): Promise<Media[]> => {
      const queue = Array.from(files);
      if (queue.length === 0) {
        return [];
      }
      setUploading(true);
      const done: Media[] = [];
      try {
        for (const file of queue) {
          try {
            done.push(await uploadMedia(file));
          } catch (err) {
            toast.error(
              `${file.name}：${err instanceof Error ? err.message : "上传失败"}`,
            );
          }
        }
      } finally {
        setUploading(false);
      }
      if (done.length > 0) {
        toast.success(
          queue.length === 1 ? "已上传" : `已上传 ${done.length} 个文件`,
        );
        await queryClient.invalidateQueries({ queryKey: ["media"] });
        uploaded.current?.(done);
      }
      return done;
    },
    [],
  );

  return { uploading, uploadFiles };
}
