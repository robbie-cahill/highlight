import AttachmentList from '@components/Comment/AttachmentList/AttachmentList';
import MenuItem from '@components/Menu/MenuItem';
import NewIssueModal from '@components/NewIssueModal/NewIssueModal';
import { namedOperations } from '@graph/operations';
import SvgFileText2Icon from '@icons/FileText2Icon';
import SvgTrashIcon from '@icons/TrashIcon';
import { ErrorCommentButton } from '@pages/Error/components/ErrorComments/ErrorCommentButton/ErrorCommentButton';
import { LINEAR_INTEGRATION } from '@pages/IntegrationsPage/Integrations';
import { getErrorTitle } from '@util/errors/errorUtils';
import { Menu } from 'antd';
import { H } from 'highlight.run';
import React, { useMemo, useState } from 'react';

import { CommentHeader } from '../../../../components/Comment/CommentHeader';
import { useDeleteErrorCommentMutation } from '../../../../graph/generated/hooks';
import CommentTextBody from '../../../Player/Toolbar/NewCommentForm/CommentTextBody/CommentTextBody';
import styles from '../../ErrorPage.module.scss';

interface Props {
    parentRef?: React.RefObject<HTMLDivElement>;
    onClickCreateComment?: () => void;
}
const ErrorComments = ({ onClickCreateComment }: Props) => {
    return (
        <>
            <div
                className={styles.actionButtonsContainer}
                style={{ margin: 0 }}
            >
                <div className={styles.actionButtons}>
                    <ErrorCommentButton
                        trackingId="CreateErrorCommentSide"
                        onClick={onClickCreateComment ?? (() => {})}
                    />
                </div>
            </div>
        </>
    );
};

export const ErrorCommentCard = ({ comment, errorGroup }: any) => (
    <div className={styles.commentDiv}>
        <ErrorCommentHeader comment={comment} errorGroup={errorGroup}>
            <CommentTextBody commentText={comment.text} />
        </ErrorCommentHeader>
        {comment.attachments.length > 0 && (
            <AttachmentList attachments={comment.attachments} />
        )}
    </div>
);

const ErrorCommentHeader = ({ comment, children, errorGroup }: any) => {
    const [deleteSessionComment] = useDeleteErrorCommentMutation({
        refetchQueries: [namedOperations.Query.GetErrorComments],
    });

    const [showNewIssueModal, setShowNewIssueModal] = useState(false);

    const defaultIssueTitle = useMemo(() => {
        if (errorGroup?.error_group?.event) {
            return getErrorTitle(errorGroup?.error_group?.event);
        }
        return `Issue from this bug error`;
    }, [errorGroup]);

    const createIssueMenuItem = (
        <MenuItem
            icon={<SvgFileText2Icon />}
            onClick={() => {
                H.track('Create Issue from Comment');
                setShowNewIssueModal(true);
            }}
        >
            Create Issue from Comment
        </MenuItem>
    );

    const moreMenu = (
        <Menu>
            {createIssueMenuItem}
            <MenuItem
                icon={<SvgTrashIcon />}
                onClick={() => {
                    deleteSessionComment({
                        variables: {
                            id: comment.id,
                        },
                    });
                }}
            >
                Delete comment
            </MenuItem>
        </Menu>
    );

    return (
        <CommentHeader moreMenu={moreMenu} comment={comment}>
            {children}
            <NewIssueModal
                selectedIntegration={LINEAR_INTEGRATION}
                visible={showNewIssueModal}
                changeVisible={setShowNewIssueModal}
                commentId={parseInt(comment.id, 10)}
                commentText={comment.text}
                commentType="ErrorComment"
                defaultIssueTitle={defaultIssueTitle || ''}
            />
        </CommentHeader>
    );
};

export default ErrorComments;
